package pluginlegacy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateCreatesCompatibleSkillAdapter(t *testing.T) {
	pluginDir := filepath.Join(t.TempDir(), "demo-plugin")
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	pluginJSON := `{"name":"demo-plugin","description":"Demo","version":"1.2.3","actions":{"echo":{"description":"Echo","command":"printf '{\"ok\":true}'","output":"json","input_schema":{"type":"object","additionalProperties":true}}},"secrets":[]}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Migrate(pluginDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(result.SkillDir, "agentdock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), "apiVersion: agentdock.dev/v1") || !strings.Contains(string(manifest), "name: \"echo\"") {
		t.Fatalf("unexpected manifest:\n%s", manifest)
	}
	info, err := os.Stat(filepath.Join(result.SkillDir, "legacy-runner.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatal("legacy runner is not executable")
	}
	if len(result.Warnings) != 1 || result.Warnings[0] != MigrationWarning {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestLoadRejectsEscapingWorkdir(t *testing.T) {
	dir := t.TempDir()
	data := `{"name":"bad-plugin","version":"1.0.0","actions":{"run":{"command":"true","workdir":"../outside"}}}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("Load accepted escaping workdir")
	}
}
