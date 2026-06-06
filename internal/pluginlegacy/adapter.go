package pluginlegacy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const MigrationWarning = "legacy AgentDock plugin compatibility mode is deprecated; migrate to a native agentdock.yaml Skill"

type Definition struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Actions     map[string]Action `json:"actions"`
	Secrets     []string          `json:"secrets"`
}

type Action struct {
	Description string         `json:"description"`
	Command     string         `json:"command"`
	Workdir     string         `json:"workdir"`
	TimeoutMS   int            `json:"timeout_ms"`
	Output      string         `json:"output"`
	InputSchema map[string]any `json:"input_schema"`
}

type Result struct {
	SkillDir string   `json:"skill_dir"`
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Warnings []string `json:"warnings"`
}

func Load(pluginDir string) (Definition, error) {
	data, err := os.ReadFile(filepath.Join(pluginDir, "plugin.json"))
	if err != nil {
		return Definition{}, fmt.Errorf("read plugin.json: %w", err)
	}
	var definition Definition
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&definition); err != nil {
		return Definition{}, fmt.Errorf("decode plugin.json: %w", err)
	}
	if err := validate(definition); err != nil {
		return Definition{}, err
	}
	return definition, nil
}

func Migrate(pluginDir, destinationRoot string) (Result, error) {
	definition, err := Load(pluginDir)
	if err != nil {
		return Result{}, err
	}
	if destinationRoot == "" {
		return Result{}, errors.New("destination root is required")
	}
	destination := filepath.Join(destinationRoot, definition.Name)
	if _, err := os.Stat(destination); err == nil {
		return Result{}, fmt.Errorf("destination already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return Result{}, err
	}
	if err := copyTree(pluginDir, destination); err != nil {
		return Result{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(destination)
		}
	}()
	manifest, err := buildManifest(definition)
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(destination, "agentdock.yaml"), []byte(manifest), 0o600); err != nil {
		return Result{}, fmt.Errorf("write agentdock.yaml: %w", err)
	}
	runner := buildRunner(definition)
	if err := os.WriteFile(filepath.Join(destination, "legacy-runner.sh"), []byte(runner), 0o700); err != nil {
		return Result{}, fmt.Errorf("write legacy runner: %w", err)
	}
	warning := MigrationWarning + "\n"
	if err := os.WriteFile(filepath.Join(destination, "MIGRATION_WARNING.txt"), []byte(warning), 0o600); err != nil {
		return Result{}, err
	}
	cleanup = false
	return Result{SkillDir: destination, Name: definition.Name, Version: normalizeVersion(definition.Version), Warnings: []string{MigrationWarning}}, nil
}

func validate(definition Definition) error {
	if !validName(definition.Name) {
		return errors.New("plugin name is invalid")
	}
	if len(definition.Actions) == 0 {
		return errors.New("plugin must define at least one action")
	}
	for name, action := range definition.Actions {
		if !validOperation(name) {
			return fmt.Errorf("action name %q is invalid", name)
		}
		if strings.TrimSpace(action.Command) == "" {
			return fmt.Errorf("action %s command is required", name)
		}
		if action.Workdir != "" {
			clean := filepath.Clean(action.Workdir)
			if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
				return fmt.Errorf("action %s workdir escapes plugin root", name)
			}
		}
	}
	return nil
}

func buildManifest(definition Definition) (string, error) {
	actions := make([]string, 0, len(definition.Actions))
	for name := range definition.Actions {
		actions = append(actions, name)
	}
	sort.Strings(actions)
	description := strings.TrimSpace(definition.Description)
	if description == "" {
		description = "Legacy plugin adapter for " + definition.Name
	}
	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: agentdock.dev/v1\nkind: Skill\nmetadata:\n")
	fmt.Fprintf(&b, "  name: %s\n", quote(definition.Name))
	fmt.Fprintf(&b, "  version: %s\n", quote(normalizeVersion(definition.Version)))
	fmt.Fprintf(&b, "  displayName: %s\n", quote(definition.Name+" (Legacy Adapter)"))
	fmt.Fprintf(&b, "  description: %s\n", quote(description))
	b.WriteString("spec:\n  entrypoint: legacy-runner.sh\n  operations:\n")
	for _, name := range actions {
		action := definition.Actions[name]
		timeout := action.TimeoutMS / 1000
		if timeout <= 0 {
			timeout = 30
		}
		inputSchema := action.InputSchema
		if len(inputSchema) == 0 {
			inputSchema = map[string]any{"type": "object", "additionalProperties": true}
		}
		inputJSON, err := json.Marshal(inputSchema)
		if err != nil {
			return "", err
		}
		outputSchema := map[string]any{"type": "object", "additionalProperties": true}
		outputJSON, _ := json.Marshal(outputSchema)
		description := strings.TrimSpace(action.Description)
		if description == "" {
			description = "Legacy action " + name
		}
		fmt.Fprintf(&b, "    - name: %s\n", quote(name))
		fmt.Fprintf(&b, "      description: %s\n", quote(description))
		fmt.Fprintf(&b, "      inputSchema: %s\n", inputJSON)
		fmt.Fprintf(&b, "      outputSchema: %s\n", outputJSON)
		fmt.Fprintf(&b, "      timeoutSeconds: %d\n", timeout)
	}
	b.WriteString("  compatibility:\n    platforms: [darwin, linux]\n    architectures: [arm64, amd64]\n    agentdock: \">=1.0.0\"\n")
	b.WriteString("  permissions:\n    filesystem: []\n    network: []\n    secrets:")
	if len(definition.Secrets) == 0 {
		b.WriteString(" []\n")
	} else {
		b.WriteByte('\n')
		for _, secret := range definition.Secrets {
			fmt.Fprintf(&b, "      - %s\n", quote(secret))
		}
	}
	b.WriteString("    commands: [sh]\n")
	return b.String(), nil
}

func buildRunner(definition Definition) string {
	actions := make([]string, 0, len(definition.Actions))
	for name := range definition.Actions {
		actions = append(actions, name)
	}
	sort.Strings(actions)
	var b strings.Builder
	b.WriteString("#!/bin/sh\nset -eu\ninput=$(cat)\nexport PLUGIN_ARGS_JSON=\"$input\"\nexport PLUGIN_NAME=")
	b.WriteString(shellQuote(definition.Name))
	b.WriteString("\ncase \"${AGENTDOCK_OPERATION:-}\" in\n")
	for _, name := range actions {
		action := definition.Actions[name]
		b.WriteString("  ")
		b.WriteString(shellQuote(name))
		b.WriteString(")\n    export PLUGIN_ACTION=")
		b.WriteString(shellQuote(name))
		b.WriteString("\n")
		if action.Workdir != "" {
			b.WriteString("    cd ")
			b.WriteString(shellQuote(action.Workdir))
			b.WriteString("\n")
		}
		b.WriteString("    output=$(sh -c ")
		b.WriteString(shellQuote(action.Command))
		b.WriteString(")\n")
		if action.Output == "json" {
			b.WriteString("    printf '%s\\n' \"$output\"\n")
		} else {
			b.WriteString("    escaped=$(printf '%s' \"$output\" | sed 's/\\\\/\\\\\\\\/g; s/\"/\\\\\"/g')\n")
			b.WriteString("    printf '{\"stdout\":\"%s\"}\\n' \"$escaped\"\n")
		}
		b.WriteString("    ;;\n")
	}
	b.WriteString("  *) echo '{\"error\":\"unknown legacy operation\"}' >&2; exit 64 ;;\nesac\n")
	return b.String()
}

func copyTree(source, destination string) error {
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(sourceAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == sourceAbs {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("legacy plugin symlink is not supported: %s", path)
		}
		rel, err := filepath.Rel(sourceAbs, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm()&0o755)
	})
}

func normalizeVersion(version string) string {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(version, ".")
	if len(parts) == 3 {
		return version
	}
	return "0.0.0-legacy"
}

func validName(value string) bool {
	if len(value) < 2 || len(value) > 63 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validOperation(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("._-", r) {
			continue
		}
		return false
	}
	return true
}

func quote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
