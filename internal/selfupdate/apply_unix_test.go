//go:build !windows

package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyPlatformUpdateReplacesBinaryAndKeepsBackup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agentdock")
	staged := filepath.Join(dir, "staged-agentdock")
	writeVersionScript(t, target, "v0.4.4")
	writeVersionScript(t, staged, "v0.4.5")

	result, err := applyPlatformUpdate(context.Background(), applyRequest{
		CurrentPath:   target,
		StagedPath:    staged,
		TargetVersion: "v0.4.5",
		Output:        os.Stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Restarted || result.HandedOff {
		t.Fatalf("unexpected managed result: %#v", result)
	}
	assertVersionScript(t, target, "v0.4.5")
	assertVersionScript(t, target+".backup", "v0.4.4")
}

func TestApplyPlatformUpdateRestoresBackupWhenNewBinaryIsInvalid(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agentdock")
	staged := filepath.Join(dir, "staged-agentdock")
	writeVersionScript(t, target, "v0.4.4")
	writeVersionScript(t, staged, "v9.9.9")

	_, err := applyPlatformUpdate(context.Background(), applyRequest{
		CurrentPath:   target,
		StagedPath:    staged,
		TargetVersion: "v0.4.5",
		Output:        os.Stdout,
	})
	if err == nil || !strings.Contains(err.Error(), "已自动恢复旧版本") {
		t.Fatalf("unexpected error: %v", err)
	}
	assertVersionScript(t, target, "v0.4.4")
	if _, statErr := os.Stat(target + ".backup"); !os.IsNotExist(statErr) {
		t.Fatalf("backup should have been restored, stat error: %v", statErr)
	}
}

func writeVersionScript(t *testing.T, path, version string) {
	t.Helper()
	content := "#!/bin/sh\nprintf 'AgentDock " + version + "\\n'\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertVersionScript(t *testing.T, path, version string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), version) {
		t.Fatalf("%s does not contain %s: %s", path, version, string(data))
	}
}
