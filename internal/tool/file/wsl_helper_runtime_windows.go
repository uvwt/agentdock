//go:build windows

package file

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	processcontrol "github.com/uvwt/agentdock/internal/process"
)

const (
	wslHelperProtocolVersion = "1"
	wslHelperOverrideEnv     = "AGENTDOCK_WSL_HELPER_PATH"
	wslHelperManifestName    = "manifest.json"
	wslHelperDeployTimeout   = 30 * time.Second
)

type wslHelperManifest struct {
	ProtocolVersion string                           `json:"protocol_version"`
	Helpers         map[string]wslHelperManifestItem `json:"helpers"`
}

type wslHelperManifestItem struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

type wslHelperResolved struct {
	WSLExecutable string
	WSLPath       string
}

type wslCommandResult struct {
	Stdout   []byte
	Stderr   []byte
	TimedOut bool
}

var wslHelperCache = struct {
	sync.Mutex
	entries map[string]wslHelperResolved
}{entries: map[string]wslHelperResolved{}}

func wslHelperCacheKey(selection fileRuntimeSelection) string {
	return selection.Distribution + "\x00" + strings.TrimSpace(os.Getenv(wslHelperOverrideEnv))
}

func invalidateWSLHelper(selection fileRuntimeSelection) {
	wslHelperCache.Lock()
	delete(wslHelperCache.entries, wslHelperCacheKey(selection))
	wslHelperCache.Unlock()
}

func (svc *Service) ensureWSLFileHelper(ctx context.Context, selection fileRuntimeSelection) (wslHelperResolved, error) {
	cacheKey := wslHelperCacheKey(selection)
	wslHelperCache.Lock()
	defer wslHelperCache.Unlock()
	if cached, ok := wslHelperCache.entries[cacheKey]; ok {
		return cached, nil
	}

	wslPath, err := exec.LookPath("wsl.exe")
	if err != nil {
		return wslHelperResolved{}, toolErrorDetails(
			"WSL_NOT_AVAILABLE",
			"wsl.exe was not found on this Windows host",
			"runtime",
			map[string]any{"reason": err.Error()},
		)
	}

	if override := strings.TrimSpace(os.Getenv(wslHelperOverrideEnv)); override != "" {
		helperPath, err := validateWSLHelperLinuxPath(override)
		if err != nil {
			return wslHelperResolved{}, err
		}
		if err := svc.verifyWSLHelperProtocol(ctx, wslPath, selection, helperPath); err != nil {
			return wslHelperResolved{}, err
		}
		resolved := wslHelperResolved{WSLExecutable: wslPath, WSLPath: helperPath}
		wslHelperCache.entries[cacheKey] = resolved
		return resolved, nil
	}

	architecture, err := svc.detectWSLHelperArchitecture(ctx, wslPath, selection)
	if err != nil {
		return wslHelperResolved{}, err
	}
	manifest, helperDir, err := loadWSLHelperManifest()
	if err != nil {
		return wslHelperResolved{}, err
	}
	if manifest.ProtocolVersion != wslHelperProtocolVersion {
		return wslHelperResolved{}, toolErrorDetails(
			"WSL_HELPER_PROTOCOL_MISMATCH",
			"bundled WSL file helper protocol does not match this AgentDock build",
			"runtime",
			map[string]any{"expected": wslHelperProtocolVersion, "actual": manifest.ProtocolVersion},
		)
	}
	item, ok := manifest.Helpers[architecture]
	if !ok {
		return wslHelperResolved{}, toolErrorDetails(
			"WSL_HELPER_ARCH_UNSUPPORTED",
			"the bundled WSL file helper does not support the selected distribution architecture",
			"runtime",
			map[string]any{"architecture": architecture, "wsl_distribution": selection.Distribution},
		)
	}
	payloadPath, expectedHash, payload, err := loadWSLHelperPayload(helperDir, item)
	if err != nil {
		return wslHelperResolved{}, err
	}
	targetPath, err := svc.resolveWSLHelperTargetPath(ctx, wslPath, selection)
	if err != nil {
		return wslHelperResolved{}, err
	}

	if hash, probeErr := svc.probeWSLHelperHash(ctx, wslPath, selection, targetPath); probeErr == nil && hash == expectedHash {
		if err := svc.verifyWSLHelperProtocol(ctx, wslPath, selection, targetPath); err == nil {
			resolved := wslHelperResolved{WSLExecutable: wslPath, WSLPath: targetPath}
			wslHelperCache.entries[cacheKey] = resolved
			return resolved, nil
		}
	}

	if err := svc.deployWSLHelper(ctx, wslPath, selection, targetPath, expectedHash, payload); err != nil {
		return wslHelperResolved{}, toolErrorDetails(
			"WSL_HELPER_INSTALL_FAILED",
			"could not install the bundled AgentDock WSL file helper",
			"runtime",
			map[string]any{
				"wsl_distribution": selection.Distribution,
				"payload":          payloadPath,
				"target":           targetPath,
				"reason":           err.Error(),
			},
		)
	}
	if err := svc.verifyWSLHelperProtocol(ctx, wslPath, selection, targetPath); err != nil {
		return wslHelperResolved{}, err
	}
	resolved := wslHelperResolved{WSLExecutable: wslPath, WSLPath: targetPath}
	wslHelperCache.entries[cacheKey] = resolved
	return resolved, nil
}

func validateWSLHelperLinuxPath(raw string) (string, error) {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.ContainsRune(raw, 0) {
		return "", toolErrorDetails(
			"WSL_HELPER_PATH_INVALID",
			"the WSL helper override must be an absolute Linux path",
			"validation",
			map[string]any{"path": raw},
		)
	}
	return pathpkg.Clean(raw), nil
}

func loadWSLHelperManifest() (wslHelperManifest, string, error) {
	executable, err := os.Executable()
	if err != nil {
		return wslHelperManifest{}, "", fmt.Errorf("resolve AgentDock executable: %w", err)
	}
	helperDir := filepath.Join(filepath.Dir(executable), "wsl-helper")
	manifestPath := filepath.Join(helperDir, wslHelperManifestName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return wslHelperManifest{}, "", toolErrorDetails(
			"WSL_HELPER_PAYLOAD_NOT_FOUND",
			"this AgentDock installation does not contain the WSL file helper payload",
			"runtime",
			map[string]any{"manifest": manifestPath, "reason": err.Error()},
		)
	}
	var manifest wslHelperManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return wslHelperManifest{}, "", toolErrorDetails(
			"WSL_HELPER_MANIFEST_INVALID",
			"the bundled WSL file helper manifest is invalid",
			"runtime",
			map[string]any{"manifest": manifestPath, "reason": err.Error()},
		)
	}
	if manifest.ProtocolVersion == "" || len(manifest.Helpers) == 0 {
		return wslHelperManifest{}, "", toolErrorDetails(
			"WSL_HELPER_MANIFEST_INVALID",
			"the bundled WSL file helper manifest is incomplete",
			"runtime",
			map[string]any{"manifest": manifestPath},
		)
	}
	return manifest, helperDir, nil
}

func loadWSLHelperPayload(helperDir string, item wslHelperManifestItem) (string, string, []byte, error) {
	if item.File == "" || filepath.Base(item.File) != item.File || strings.ContainsAny(item.File, `/\\`) {
		return "", "", nil, toolErrorDetails(
			"WSL_HELPER_MANIFEST_INVALID",
			"the bundled WSL file helper manifest contains an invalid payload name",
			"runtime",
			map[string]any{"file": item.File},
		)
	}
	expectedHash := strings.ToLower(strings.TrimSpace(item.SHA256))
	hashBytes, err := hex.DecodeString(expectedHash)
	if err != nil || len(hashBytes) != sha256.Size {
		return "", "", nil, toolErrorDetails(
			"WSL_HELPER_MANIFEST_INVALID",
			"the bundled WSL file helper manifest contains an invalid SHA-256 digest",
			"runtime",
			map[string]any{"file": item.File},
		)
	}
	payloadPath := filepath.Join(helperDir, item.File)
	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		return "", "", nil, toolErrorDetails(
			"WSL_HELPER_PAYLOAD_NOT_FOUND",
			"the selected WSL file helper payload is missing",
			"runtime",
			map[string]any{"payload": payloadPath, "reason": err.Error()},
		)
	}
	actual := sha256.Sum256(payload)
	actualHash := hex.EncodeToString(actual[:])
	if actualHash != expectedHash {
		return "", "", nil, toolErrorDetails(
			"WSL_HELPER_INTEGRITY_FAILED",
			"the bundled WSL file helper payload failed SHA-256 verification",
			"runtime",
			map[string]any{"payload": payloadPath, "expected": expectedHash, "actual": actualHash},
		)
	}
	return payloadPath, expectedHash, payload, nil
}

func (svc *Service) detectWSLHelperArchitecture(ctx context.Context, wslPath string, selection fileRuntimeSelection) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := svc.runWSLCommand(probeCtx, wslPath, selection, nil, "uname", "-m")
	if err != nil {
		return "", toolErrorDetails(
			"WSL_HELPER_ARCH_DETECTION_FAILED",
			"could not detect the selected WSL distribution architecture",
			"runtime",
			map[string]any{"wsl_distribution": selection.Distribution, "reason": err.Error(), "stderr": truncateString(strings.TrimSpace(string(result.Stderr)), 2000)},
		)
	}
	arch := strings.ToLower(strings.TrimSpace(string(result.Stdout)))
	switch arch {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", toolErrorDetails(
			"WSL_HELPER_ARCH_UNSUPPORTED",
			"the selected WSL distribution architecture is not supported by AgentDock",
			"runtime",
			map[string]any{"architecture": arch, "wsl_distribution": selection.Distribution},
		)
	}
}

func (svc *Service) resolveWSLHelperTargetPath(ctx context.Context, wslPath string, selection fileRuntimeSelection) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	const script = `state=${XDG_STATE_HOME:-}; case "$state" in /*) ;; *) state="$HOME/.local/state" ;; esac; printf '%s' "$state/agentdock/bin/agentdock-wsl-helper"`
	result, err := svc.runWSLCommand(probeCtx, wslPath, selection, nil, "/bin/sh", "-c", script)
	if err != nil {
		return "", toolErrorDetails(
			"WSL_HELPER_STATE_PATH_FAILED",
			"could not resolve the AgentDock state directory in the selected WSL distribution",
			"runtime",
			map[string]any{"wsl_distribution": selection.Distribution, "reason": err.Error(), "stderr": truncateString(strings.TrimSpace(string(result.Stderr)), 2000)},
		)
	}
	return validateWSLHelperLinuxPath(strings.TrimSpace(string(result.Stdout)))
}

func (svc *Service) probeWSLHelperHash(ctx context.Context, wslPath string, selection fileRuntimeSelection, helperPath string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := svc.runWSLCommand(probeCtx, wslPath, selection, nil, helperPath, "--self-sha256")
	if err != nil {
		return "", err
	}
	hash := strings.ToLower(strings.TrimSpace(string(result.Stdout)))
	decoded, decodeErr := hex.DecodeString(hash)
	if decodeErr != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("helper returned invalid SHA-256 %q", hash)
	}
	return hash, nil
}

func (svc *Service) verifyWSLHelperProtocol(ctx context.Context, wslPath string, selection fileRuntimeSelection, helperPath string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := svc.runWSLCommand(probeCtx, wslPath, selection, nil, helperPath, "--protocol-version")
	if err != nil {
		return toolErrorDetails(
			"WSL_HELPER_START_FAILED",
			"the AgentDock WSL file helper could not be started",
			"runtime",
			map[string]any{"wsl_distribution": selection.Distribution, "helper": helperPath, "reason": err.Error(), "stderr": truncateString(strings.TrimSpace(string(result.Stderr)), 2000)},
		)
	}
	actual := strings.TrimSpace(string(result.Stdout))
	if actual != wslHelperProtocolVersion {
		return toolErrorDetails(
			"WSL_HELPER_PROTOCOL_MISMATCH",
			"the AgentDock WSL file helper protocol is incompatible with this AgentDock build",
			"runtime",
			map[string]any{"expected": wslHelperProtocolVersion, "actual": actual, "helper": helperPath},
		)
	}
	return nil
}

func (svc *Service) deployWSLHelper(
	ctx context.Context,
	wslPath string,
	selection fileRuntimeSelection,
	targetPath string,
	expectedHash string,
	payload []byte,
) error {
	deployCtx, cancel := context.WithTimeout(ctx, wslHelperDeployTimeout)
	defer cancel()
	const script = `set -eu
target=$1
dir=${target%/*}
mkdir -p "$dir"
tmp=$(mktemp "$dir/.agentdock-wsl-helper.tmp.XXXXXX")
trap 'rm -f "$tmp"' EXIT HUP INT TERM
cat > "$tmp"
chmod 0755 "$tmp"
mv -f "$tmp" "$target"
trap - EXIT HUP INT TERM
exec "$target" --self-sha256`
	result, err := svc.runWSLCommand(deployCtx, wslPath, selection, payload, "/bin/sh", "-c", script, "agentdock-wsl-helper-install", targetPath)
	if err != nil {
		if result.TimedOut {
			return fmt.Errorf("WSL helper installation exceeded %s", wslHelperDeployTimeout)
		}
		return fmt.Errorf("deploy helper: %w: %s", err, truncateString(strings.TrimSpace(string(result.Stderr)), 2000))
	}
	actualHash := strings.ToLower(strings.TrimSpace(string(result.Stdout)))
	if actualHash != expectedHash {
		return fmt.Errorf("deployed helper SHA-256 mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	return nil
}

func (svc *Service) runWSLCommand(
	ctx context.Context,
	wslPath string,
	selection fileRuntimeSelection,
	stdin []byte,
	command string,
	commandArgs ...string,
) (wslCommandResult, error) {
	args := make([]string, 0, len(commandArgs)+5)
	if selection.Distribution != "" {
		args = append(args, "--distribution", selection.Distribution)
	}
	args = append(args, "--exec", command)
	args = append(args, commandArgs...)

	commandEnv, err := svc.commandEnv("", nil)
	if err != nil {
		return wslCommandResult{}, err
	}
	cmd := exec.CommandContext(ctx, wslPath, args...)
	cmd.Dir = svc.ws.DefaultCWD()
	cmd.Env = commandEnv
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	processcontrol.Configure(cmd)
	err = cmd.Run()
	return wslCommandResult{
		Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), TimedOut: ctx.Err() == context.DeadlineExceeded,
	}, err
}
