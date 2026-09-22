package command

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestEnsureManagedSkillDataDirCreatesPrivateStablePath(t *testing.T) {
	svc, cfg := newCommandTestService(t)

	got, err := svc.ensureManagedSkillDataDir("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	want, err := config.SkillDataDir(*cfg, "demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("data dir = %q, want %q", got, want)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("data path is not a directory: %s", got)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("data directory mode = %o, want 700", info.Mode().Perm())
	}

	if err := os.Chmod(got, 0o755); err == nil && runtime.GOOS != "windows" {
		if _, err := svc.ensureManagedSkillDataDir("demo-skill"); err != nil {
			t.Fatal(err)
		}
		info, err = os.Stat(got)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("existing data directory mode = %o after repair, want 700", info.Mode().Perm())
		}
	}
}

func TestEnsureManagedSkillDataDirRejectsSymlinkedDataRoot(t *testing.T) {
	svc, cfg := newCommandTestService(t)
	outside := t.TempDir()
	dataRoot := filepath.Join(cfg.AgentDockHome, "data")
	if err := os.Symlink(outside, dataRoot); err != nil {
		t.Skipf("symlink unavailable on this platform: %v", err)
	}

	if _, err := svc.ensureManagedSkillDataDir("demo-skill"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ensureManagedSkillDataDir() error = %v, want symlink rejection", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "skills", "demo-skill")); !os.IsNotExist(err) {
		t.Fatalf("data directory escaped through symlink: %v", err)
	}
}
