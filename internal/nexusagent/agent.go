package nexusagent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	contracts "github.com/uvwt/agentdock/generated/nexuscontracts"
	"github.com/uvwt/agentdock/internal/artifactrelay"
	"github.com/uvwt/agentdock/internal/commandqueue"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envregistry"
	"github.com/uvwt/agentdock/internal/nexusclient"
	"github.com/uvwt/agentdock/internal/skillruntime"
	"github.com/uvwt/agentdock/internal/skillstate"
)

// Start wires the optional Nexus background agent. It is disabled unless
// AGENTDOCK_NEXUS_ENDPOINT is configured.
func Start(ctx context.Context, cfg config.Config) (bool, error) {
	endpoint := strings.TrimSpace(cfg.NexusEndpoint)
	if endpoint == "" {
		return false, nil
	}

	stateDir, err := resolveStateDir(cfg)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return false, fmt.Errorf("create nexus state dir: %w", err)
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		return false, fmt.Errorf("secure nexus state dir: %w", err)
	}

	client, err := nexusclient.New(nexusclient.Config{
		BaseURL:        endpoint,
		RequestTimeout: 15 * time.Second,
		PollTimeout:    35 * time.Second,
		UserAgent:      "agentdock/" + config.Version,
	})
	if err != nil {
		return false, err
	}
	deviceState, err := nexusclient.OpenStateStore(filepath.Join(stateDir, "device"))
	if err != nil {
		return false, err
	}
	outbox, err := commandqueue.OpenOutbox(filepath.Join(stateDir, "queue"))
	if err != nil {
		return false, err
	}
	commandStore, err := commandqueue.OpenStore(filepath.Join(stateDir, "queue"))
	if err != nil {
		return false, err
	}
	executor := commandqueue.NewExecutor(commandStore)
	skillStore, err := skillstate.New(filepath.Join(stateDir, "skills"))
	if err != nil {
		return false, err
	}
	bindingStore, err := skillruntime.NewBindingStore(filepath.Join(stateDir, "bindings"))
	if err != nil {
		return false, err
	}
	skillRuntime, err := skillruntime.New(skillStore, bindingStore)
	if err != nil {
		return false, err
	}
	envStore, err := envregistry.New(filepath.Join(stateDir, "env"), func() []envregistry.Definition {
		return envDefinitions(skillStore)
	})
	if err != nil {
		return false, err
	}
	skillRuntime.EnvProvider = nexusEnvProvider{store: envStore}
	artifactKeys, err := artifactrelay.EnsureKeyPair(filepath.Join(stateDir, "artifact-key"))
	if err != nil {
		return false, err
	}
	artifactTargets, err := artifactrelay.ParseTargetsJSON(cfg.ArtifactTargetsJSON)
	if err != nil {
		return false, err
	}
	artifactReceiver, err := artifactrelay.NewReceiver(artifactrelay.ReceiverConfig{
		Client: client,
		Credentials: func() (artifactrelay.DeviceCredentials, error) {
			state, err := deviceState.Load()
			return artifactrelay.DeviceCredentials{DeviceID: state.DeviceID, DeviceToken: state.DeviceToken}, err
		},
		PrivateKey: artifactKeys.Private,
		InboxRoot:  filepath.Join(stateDir, "artifacts", "inbox"),
		Targets:    artifactTargets,
	})
	if err != nil {
		return false, err
	}
	var artifactFetcher *artifactrelay.SourceFetcher
	if cfg.ArtifactFetchEnabled {
		denyPaths, err := artifactrelay.ParseFetchDenyJSON(cfg.ArtifactFetchDenyJSON)
		if err != nil {
			return false, err
		}
		artifactFetcher, err = artifactrelay.NewSourceFetcher(artifactrelay.SourceFetcherConfig{
			Client: client,
			Credentials: func() (artifactrelay.DeviceCredentials, error) {
				state, err := deviceState.Load()
				return artifactrelay.DeviceCredentials{DeviceID: state.DeviceID, DeviceToken: state.DeviceToken}, err
			},
			TempRoot:            filepath.Join(stateDir, "artifacts", "fetch-source"),
			AdditionalDenyPaths: denyPaths,
			StateDir:            stateDir,
		})
		if err != nil {
			return false, err
		}
	}

	if err := commandqueue.RegisterAdapters(executor, commandqueue.AdapterDependencies{
		Health:        healthChecker{cfg: cfg},
		Skills:        skillRouter{runtime: skillRuntime},
		Env:           envRouter{store: envStore, runtime: skillRuntime},
		Artifacts:     artifactReceiverAdapter{receiver: artifactReceiver},
		ArtifactFetch: artifactFetcherAdapter{fetcher: artifactFetcher},
	}); err != nil {
		return false, err
	}

	heartbeat := nexusclient.SystemHeartbeatProvider{
		StartedAt: time.Now().UTC(),
		Version:   config.Version,
		Capabilities: func() []contracts.DeviceCapability {
			return capabilities(cfg, artifactKeys.PublicEncoded())
		},
		SkillSummary: func() any {
			return map[string]any{"runtime": "enabled", "state_dir": filepath.Base(stateDir)}
		},
		RecallSummary: func() any {
			return map[string]any{"enabled": strings.TrimSpace(cfg.RecallEndpoint) != ""}
		},
	}
	agent, err := nexusclient.NewAgent(client, deviceState, outbox, executor, heartbeat, nexusclient.AgentConfig{
		HeartbeatInterval: time.Duration(cfg.NexusHeartbeatSeconds) * time.Second,
	})
	if err != nil {
		return false, err
	}

	state, err := deviceState.Load()
	if err != nil {
		return false, err
	}
	if !state.Valid(time.Now()) {
		codePath := filepath.Join(stateDir, "enroll-code")
		codeValue, err := readOneTimeCode(codePath)
		if err != nil {
			return false, err
		}
		publicKey, err := ensureDeviceKey(filepath.Join(stateDir, "device-key"))
		if err != nil {
			return false, err
		}
		labels, _ := json.Marshal(map[string]string{"managed_by": "agentdock"})
		name := strings.TrimSpace(cfg.NexusDeviceName)
		if name == "" {
			name, _ = os.Hostname()
		}
		if name == "" {
			name = "agentdock-device"
		}
		_, err = agent.Enroll(ctx, contracts.DeviceEnrollmentRequest{
			EnrollmentToken:  codeValue,
			Name:             name,
			Platform:         runtime.GOOS,
			Arch:             runtime.GOARCH,
			AgentdockVersion: config.Version,
			PublicKey:        publicKey,
			Labels:           labels,
		})
		if err != nil {
			return false, fmt.Errorf("enroll Nexus device: %w", err)
		}
		if err := os.Remove(codePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("remove one-time enrollment code: %w", err)
		}
	}

	go func() {
		if err := agent.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("nexus agent stopped", "error", err)
		}
	}()
	return true, nil
}

func resolveStateDir(cfg config.Config) (string, error) {
	return config.ResolveNexusStateDir(cfg)
}

func ensureDeviceKey(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create device key dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("secure device key dir: %w", err)
	}
	privatePath := filepath.Join(dir, "ed25519.private")
	publicPath := filepath.Join(dir, "ed25519.public")
	if publicBytes, err := os.ReadFile(publicPath); err == nil {
		return strings.TrimSpace(string(publicBytes)), nil
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate device key: %w", err)
	}
	privateEncoded := base64.RawStdEncoding.EncodeToString(privateKey)
	publicEncoded := "ed25519:" + base64.RawStdEncoding.EncodeToString(publicKey)
	if err := os.WriteFile(privatePath, []byte(privateEncoded+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write private device key: %w", err)
	}
	if err := os.WriteFile(publicPath, []byte(publicEncoded+"\n"), 0o600); err != nil {
		_ = os.Remove(privatePath)
		return "", fmt.Errorf("write public device key: %w", err)
	}
	return publicEncoded, nil
}

func capabilities(cfg config.Config, artifactPublicKey string) []contracts.DeviceCapability {
	metadata, _ := json.Marshal(map[string]string{"goos": runtime.GOOS, "goarch": runtime.GOARCH})
	return []contracts.DeviceCapability{
		{Name: "mcp", Version: config.ProtocolVersion, Enabled: true, Metadata: metadata},
		{Name: "recall", Version: "v1", Enabled: strings.TrimSpace(cfg.RecallEndpoint) != ""},
		{Name: "skill-runtime", Version: "v1", Enabled: true},
		{Name: "browser", Version: "v1", Enabled: cfg.BrowserEnabled},
		{Name: "artifact-relay", Version: artifactrelay.FormatVersion, Enabled: true, Metadata: mustJSON(map[string]string{"x25519_public_key": artifactPublicKey, "max_cipher_bytes": fmt.Sprint(artifactrelay.MaxCipherBytes), "fetch_enabled": fmt.Sprint(cfg.ArtifactFetchEnabled)})},
	}
}

type artifactReceiverAdapter struct{ receiver *artifactrelay.Receiver }

func (a artifactReceiverAdapter) Pull(ctx context.Context, payload json.RawMessage) (any, error) {
	return a.receiver.Pull(ctx, payload)
}

type artifactFetcherAdapter struct{ fetcher *artifactrelay.SourceFetcher }

func (a artifactFetcherAdapter) Fetch(ctx context.Context, payload json.RawMessage) (any, error) {
	if a.fetcher == nil {
		return nil, errors.New("artifact fetch is disabled on this device")
	}
	return a.fetcher.Fetch(ctx, payload)
}

func mustJSON(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

type healthChecker struct{ cfg config.Config }

func (h healthChecker) Health(context.Context) (any, error) {
	return map[string]any{
		"ok":              true,
		"version":         config.Version,
		"platform":        runtime.GOOS,
		"arch":            runtime.GOARCH,
		"recall_enabled":  strings.TrimSpace(h.cfg.RecallEndpoint) != "",
		"browser_enabled": h.cfg.BrowserEnabled,
	}, nil
}

type skillRouter struct{ runtime *skillruntime.Runtime }

type nexusEnvProvider struct{ store *envregistry.Store }

func (p nexusEnvProvider) EnvForSkill(skill string, definitions []skillruntime.EnvDefinition) (map[string]string, []string, error) {
	items := make([]envregistry.Definition, 0, len(definitions))
	for _, def := range definitions {
		items = append(items, envregistry.Definition{Skill: def.Skill, Name: def.Name, Kind: def.Kind, Source: def.Source})
	}
	return p.store.EnvForSkill(skill, items)
}

type envRouter struct {
	store   *envregistry.Store
	runtime *skillruntime.Runtime
}

func (r envRouter) ExecuteEnvCommand(ctx context.Context, payload json.RawMessage, _ commandqueue.ProgressReporter) (commandqueue.HandlerResult, error) {
	var request struct {
		Action         string `json:"action"`
		Skill          string `json:"skill,omitempty"`
		Name           string `json:"name,omitempty"`
		Kind           string `json:"kind,omitempty"`
		Value          string `json:"value,omitempty"`
		Operation      string `json:"operation,omitempty"`
		TimeoutMS      int    `json:"timeout_ms,omitempty"`
		MaxOutputBytes int    `json:"max_output_bytes,omitempty"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		return commandqueue.HandlerResult{}, err
	}
	action := strings.ToLower(strings.TrimSpace(request.Action))
	switch action {
	case "list":
		result, err := r.store.List()
		return commandqueue.HandlerResult{Output: map[string]any{"ok": err == nil, "action": action, "skills": result, "count": len(result)}}, err
	case "inspect":
		result, err := r.store.Inspect(request.Skill)
		return commandqueue.HandlerResult{Output: map[string]any{"ok": err == nil, "action": action, "skill": request.Skill, "vars": result, "count": len(result)}}, err
	case "set":
		entry, err := r.store.Set(request.Skill, request.Name, request.Kind, request.Value)
		return commandqueue.HandlerResult{Output: map[string]any{"ok": err == nil, "action": action, "skill": request.Skill, "var": entry}}, err
	case "delete":
		deleted, err := r.store.Delete(request.Skill, request.Name)
		return commandqueue.HandlerResult{Output: map[string]any{"ok": err == nil, "action": action, "skill": request.Skill, "name": request.Name, "deleted": deleted}}, err
	case "verify":
		operation := strings.TrimSpace(request.Operation)
		if operation == "" {
			operation = "status"
		}
		run, err := r.runtime.Run(ctx, skillruntime.RunRequest{
			Skill: request.Skill, Operation: operation, Input: json.RawMessage(`{}`),
			Timeout: time.Duration(request.TimeoutMS) * time.Millisecond, MaxOutput: request.MaxOutputBytes,
		})
		ok := err == nil && run.OK
		message := "ok"
		if err != nil {
			message = err.Error()
		} else if !run.OK {
			message = run.Error
		}
		_ = r.store.RecordVerification(request.Skill, ok, message)
		return commandqueue.HandlerResult{Output: map[string]any{"ok": ok, "action": action, "skill": request.Skill, "result": run, "message": message}}, err
	default:
		return commandqueue.HandlerResult{}, fmt.Errorf("unsupported env action %q", action)
	}
}

func envDefinitions(state *skillstate.Store) []envregistry.Definition {
	result := map[string]envregistry.Definition{}
	if state != nil {
		if names, err := state.ListSkills(); err == nil {
			for _, name := range names {
				selection, err := state.Snapshot(name)
				if err != nil || selection.ActiveVersion == "" {
					continue
				}
				packageDir, err := state.InstalledPath(name, selection.ActiveVersion)
				if err != nil {
					continue
				}
				manifest, err := skillruntime.LoadManifest(packageDir)
				if err != nil {
					continue
				}
				for _, def := range skillruntime.EnvDefinitionsForManifest(manifest) {
					result[def.Skill+"\x00"+def.Name] = envregistry.Definition{Skill: def.Skill, Name: def.Name, Kind: def.Kind, Source: def.Source}
				}
			}
		}
	}
	items := make([]envregistry.Definition, 0, len(result))
	for _, def := range result {
		items = append(items, def)
	}
	return items
}

func (r skillRouter) ExecuteSkillCommand(ctx context.Context, commandType string, payload json.RawMessage, progress commandqueue.ProgressReporter) (commandqueue.HandlerResult, error) {
	if r.runtime == nil {
		return commandqueue.HandlerResult{}, errors.New("skill runtime is unavailable")
	}
	switch commandType {
	case "skill.install":
		var request struct {
			Source         string `json:"source"`
			Digest         string `json:"digest_sha256,omitempty"`
			Activate       bool   `json:"activate,omitempty"`
			Channel        string `json:"channel,omitempty"`
			MaxBytes       int64  `json:"max_bytes,omitempty"`
			ConfirmedNoEnv bool   `json:"confirmed_no_env,omitempty"`
		}
		if err := json.Unmarshal(payload, &request); err != nil {
			return commandqueue.HandlerResult{}, err
		}
		result, err := r.runtime.Install(ctx, skillruntime.InstallRequest{
			Source: request.Source, DigestSHA256: request.Digest, Activate: request.Activate,
			Channel: skillstate.Channel(request.Channel), MaxBytes: request.MaxBytes, ConfirmedNoEnv: request.ConfirmedNoEnv,
		})
		return commandqueue.HandlerResult{Output: result}, err
	case "skill.run":
		var request skillruntime.RunRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			return commandqueue.HandlerResult{}, err
		}
		result, err := r.runtime.Run(ctx, request)
		return commandqueue.HandlerResult{Output: result}, err
	case "skill.rollback":
		var request struct {
			Skill   string `json:"skill"`
			Channel string `json:"channel,omitempty"`
		}
		if err := json.Unmarshal(payload, &request); err != nil {
			return commandqueue.HandlerResult{}, err
		}
		result, err := r.runtime.Rollback(ctx, request.Skill, skillstate.Channel(request.Channel), nil)
		return commandqueue.HandlerResult{Output: result}, err
	default:
		return commandqueue.HandlerResult{}, fmt.Errorf("unsupported skill command %q", commandType)
	}
}

func readOneTimeCode(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("Nexus enrollment requires local file %s", path)
		}
		return "", fmt.Errorf("stat Nexus enrollment file: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("Nexus enrollment file %s must use mode 0600", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read Nexus enrollment file: %w", err)
	}
	value := strings.TrimSpace(string(content))
	if value == "" {
		return "", fmt.Errorf("Nexus enrollment file %s is empty", path)
	}
	return value, nil
}
