package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type connectorDefinition struct {
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Version     string                     `json:"version"`
	Actions     map[string]connectorAction `json:"actions"`
	Secrets     []string                   `json:"secrets"`
	Metadata    map[string]any             `json:"metadata"`
}

type connectorAction struct {
	Description string         `json:"description"`
	Command     string         `json:"command"`
	Workdir     string         `json:"workdir"`
	TimeoutMS   int            `json:"timeout_ms"`
	Output      string         `json:"output"`
	Env         map[string]any `json:"env"`
	InputSchema map[string]any `json:"input_schema"`
}

type connectorInfo struct {
	Name        string         `json:"name"`
	Path        string         `json:"path"`
	ConfigPath  string         `json:"config_path"`
	Description string         `json:"description"`
	Version     string         `json:"version"`
	Actions     []string       `json:"actions"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

func (r *Runtime) connectorList(args map[string]any) (Result, error) {
	root, err := r.connectorRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root.Abs)
	if os.IsNotExist(err) {
		return Result{"ok": true, "connector_dir": root.Display, "connectors": []any{}, "count": 0}, nil
	}
	if err != nil {
		return nil, err
	}
	connectors := make([]connectorInfo, 0)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		definition, configPath, err := r.loadConnector(entry.Name())
		if err != nil {
			connectors = append(connectors, connectorInfo{Name: entry.Name(), Path: filepath.ToSlash(filepath.Join(root.Display, entry.Name())), ConfigPath: configPath, Description: "invalid connector: " + err.Error()})
			continue
		}
		connectors = append(connectors, connectorSummary(root.Display, entry.Name(), configPath, definition))
	}
	sort.Slice(connectors, func(i, j int) bool { return connectors[i].Name < connectors[j].Name })
	return Result{"ok": true, "connector_dir": root.Display, "connectors": connectors, "count": len(connectors)}, nil
}

func (r *Runtime) connectorDescribe(args map[string]any) (Result, error) {
	name := stringArg(args, "connector", stringArg(args, "name", ""))
	if name == "" {
		return nil, toolError("INVALID_ARGUMENT", "connector is required", "validation")
	}
	definition, configPath, err := r.loadConnector(name)
	if err != nil {
		return nil, err
	}
	_, root, err := r.connectorDir(name)
	if err != nil {
		return nil, err
	}
	actions := make(map[string]map[string]any, len(definition.Actions))
	for actionName, action := range definition.Actions {
		actions[actionName] = map[string]any{"description": action.Description, "input_schema": action.InputSchema, "timeout_ms": action.TimeoutMS, "output": action.Output}
	}
	return Result{"ok": true, "connector": definition.Name, "path": root.Display, "config_path": configPath, "description": definition.Description, "version": definition.Version, "actions": actions, "secrets": secretPresence(definition.Secrets), "metadata": definition.Metadata}, nil
}

func (r *Runtime) connectorCall(ctx context.Context, args map[string]any) (Result, error) {
	connectorName := stringArg(args, "connector", "")
	actionName := stringArg(args, "action", "")
	if connectorName == "" || actionName == "" {
		return nil, toolError("INVALID_ARGUMENT", "connector and action are required", "validation")
	}
	definition, _, err := r.loadConnector(connectorName)
	if err != nil {
		return nil, err
	}
	action, ok := definition.Actions[actionName]
	if !ok {
		return nil, toolErrorDetails("UNKNOWN_CONNECTOR_ACTION", "connector action is not defined", "validation", map[string]any{"connector": connectorName, "action": actionName})
	}
	_, connectorDir, err := r.connectorDir(connectorName)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(action.Command) == "" {
		return nil, toolError("INVALID_CONNECTOR", "action command is required", "validation")
	}
	workdir := connectorDir.Abs
	if action.Workdir != "" {
		workdir, err = safeJoin(connectorDir.Abs, action.Workdir)
		if err != nil {
			return nil, err
		}
	}
	timeout := time.Duration(action.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout > 10*time.Minute {
		timeout = 10 * time.Minute
	}
	input := mapArg(args, "args")
	inputData, _ := json.Marshal(input)
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "/bin/sh", "-c", action.Command)
	cmd.Dir = workdir
	env := map[string]any{"CONNECTOR_NAME": connectorName, "CONNECTOR_ACTION": actionName, "CONNECTOR_ARGS_JSON": string(inputData), "WORKSPACE": r.ws.Root()}
	for key, value := range action.Env {
		env[key] = value
	}
	cmd.Env = r.commandEnv(env)
	started := time.Now()
	output, err := cmd.CombinedOutput()
	text, truncated := truncateBytes(output, intArg(args, "max_bytes", 65536))
	text = redactSecrets(text, definition.Secrets)
	result := Result{"ok": err == nil, "connector": connectorName, "action": actionName, "duration_ms": time.Since(started).Milliseconds(), "stdout": text, "truncated": truncated}
	if err != nil {
		result["error"] = err.Error()
	}
	if action.Output == "json" && text != "" {
		var parsed any
		if parseErr := json.Unmarshal([]byte(text), &parsed); parseErr == nil {
			result["json"] = parsed
		} else {
			result["json_error"] = parseErr.Error()
		}
	}
	return result, nil
}

func (r *Runtime) connectorRoot() (controlPath, error) {
	return r.resolveControlForWrite(r.cfg.ConnectorDir)
}

func (r *Runtime) connectorDir(name string) (controlPath, controlPath, error) {
	if !validConnectorName(name) {
		return controlPath{}, controlPath{}, toolError("INVALID_ARGUMENT", "connector name may contain only letters, numbers, dot, underscore, and dash", "validation")
	}
	root, err := r.connectorRoot()
	if err != nil {
		return controlPath{}, controlPath{}, err
	}
	p, err := r.resolveControlExisting(filepath.Join(r.cfg.ConnectorDir, name))
	if err != nil {
		return root, controlPath{}, err
	}
	info, err := os.Stat(p.Abs)
	if err != nil {
		return root, controlPath{}, err
	}
	if !info.IsDir() {
		return root, controlPath{}, toolError("NOT_A_DIRECTORY", "connector path is not a directory", "validation")
	}
	return root, p, nil
}

func (r *Runtime) loadConnector(name string) (connectorDefinition, string, error) {
	_, p, err := r.connectorDir(name)
	if err != nil {
		return connectorDefinition{}, "", err
	}
	configPath := filepath.Join(p.Abs, "connector.json")
	if _, err := os.Stat(configPath); err != nil {
		return connectorDefinition{}, "", toolErrorDetails("CONNECTOR_CONFIG_NOT_FOUND", "connector.json is required for now", "validation", map[string]any{"connector": name, "path": p.Display})
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return connectorDefinition{}, "", err
	}
	var definition connectorDefinition
	if err := json.Unmarshal(data, &definition); err != nil {
		return connectorDefinition{}, "", toolErrorDetails("CONNECTOR_CONFIG_INVALID", err.Error(), "validation", map[string]any{"connector": name, "path": p.Display})
	}
	if definition.Name == "" {
		definition.Name = name
	}
	if definition.Name != name {
		return connectorDefinition{}, "", toolError("CONNECTOR_NAME_MISMATCH", "connector name must match directory name", "validation")
	}
	if len(definition.Actions) == 0 {
		return connectorDefinition{}, "", toolError("CONNECTOR_HAS_NO_ACTIONS", "connector must define at least one action", "validation")
	}
	return definition, filepath.ToSlash(filepath.Join(p.Display, "connector.json")), nil
}

func connectorSummary(root, name, configPath string, definition connectorDefinition) connectorInfo {
	actions := make([]string, 0, len(definition.Actions))
	for action := range definition.Actions {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	return connectorInfo{Name: definition.Name, Path: filepath.ToSlash(filepath.Join(root, name)), ConfigPath: configPath, Description: definition.Description, Version: definition.Version, Actions: actions, Metadata: definition.Metadata}
}

func validConnectorName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.Contains(name, "/") || strings.Contains(name, `\\`) {
		return false
	}
	for _, ch := range name {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '.' {
			continue
		}
		return false
	}
	return true
}

func safeJoin(root, rel string) (string, error) {
	candidate := filepath.Clean(filepath.Join(root, rel))
	rootClean := filepath.Clean(root)
	if candidate != rootClean && !strings.HasPrefix(candidate, rootClean+string(os.PathSeparator)) {
		return "", toolError("PATH_OUTSIDE_CONNECTOR", "connector workdir escapes connector directory", "validation")
	}
	return candidate, nil
}

func secretPresence(keys []string) map[string]bool {
	out := map[string]bool{}
	for _, key := range keys {
		out[key] = os.Getenv(key) != ""
	}
	return out
}
