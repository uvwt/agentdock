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
	bundle := writeCoreSkillBundle(t, dir)
	writeVersionScript(t, target, "v0.4.4")
	writeVersionScript(t, staged, "v0.4.5")

	result, err := applyPlatformUpdate(context.Background(), applyRequest{
		CurrentPath:   target,
		StagedPath:    staged,
		BundlePath:    bundle,
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
	bundle := writeCoreSkillBundle(t, dir)
	writeVersionScript(t, target, "v0.4.4")
	writeVersionScript(t, staged, "v9.9.9")

	_, err := applyPlatformUpdate(context.Background(), applyRequest{
		CurrentPath:   target,
		StagedPath:    staged,
		BundlePath:    bundle,
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

func TestApplyPlatformUpdateRestoresBackupWhenCoreSkillBootstrapFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agentdock")
	staged := filepath.Join(dir, "staged-agentdock")
	bundle := writeCoreSkillBundle(t, dir)
	writeVersionScript(t, target, "v0.4.4")
	content := `#!/bin/sh
case "${1:-}" in
  --version) printf 'AgentDock v0.4.5\n' ;;
  skill) printf 'bootstrap failed\n' >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(staged, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := applyPlatformUpdate(context.Background(), applyRequest{
		CurrentPath:   target,
		StagedPath:    staged,
		BundlePath:    bundle,
		TargetVersion: "v0.4.5",
		Output:        os.Stdout,
	})
	if err == nil || !strings.Contains(err.Error(), "已自动恢复旧版本") {
		t.Fatalf("unexpected error: %v", err)
	}
	assertVersionScript(t, target, "v0.4.4")
}

func writeVersionScript(t *testing.T, path, version string) {
	t.Helper()
	content := `#!/bin/sh
case "${1:-}" in
  --version) printf 'AgentDock ` + version + `\n' ;;
  skill)
    [ "${2:-}" = bootstrap ] && [ "${3:-}" = --bundle ] && [ -f "${4:-}/manifest.json" ] || exit 2
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeCoreSkillBundle(t *testing.T, dir string) string {
	t.Helper()
	bundle := filepath.Join(dir, "core-skills")
	if err := os.Mkdir(bundle, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte(`{"skills":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return bundle
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
