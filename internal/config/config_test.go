package config

import (
	"path/filepath"
	"testing"
)

func TestNormalizeDefaultsToUserDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := Config{}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	wantHome := filepath.Join(home, ".agentdock")
	wantDefault := filepath.Join(home, "AgentDock")
	if cfg.AgentDockHome != wantHome {
		t.Fatalf("AgentDockHome = %q, want %q", cfg.AgentDockHome, wantHome)
	}
	if cfg.AgentDockDefaultDir != wantDefault {
		t.Fatalf("AgentDockDefaultDir = %q, want %q", cfg.AgentDockDefaultDir, wantDefault)
	}
}

func TestFromEnvIgnoresOldDirectoryConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENTDOCK_WORKSPACE", "/tmp/old-workspace")
	t.Setenv("AGENTDOCK_RUNTIME_PROFILE", "workspace")
	t.Setenv("AGENTDOCK_DIR", "/tmp/old-control")

	cfg := FromEnv()
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if cfg.AgentDockHome != filepath.Join(home, ".agentdock") {
		t.Fatalf("AgentDockHome = %q", cfg.AgentDockHome)
	}
	if cfg.AgentDockDefaultDir != filepath.Join(home, "AgentDock") {
		t.Fatalf("AgentDockDefaultDir = %q", cfg.AgentDockDefaultDir)
	}
}

func TestNexusStateDirUsesAgentDockHome(t *testing.T) {
	home := t.TempDir()
	cfg := Config{AgentDockHome: filepath.Join(home, ".agentdock")}
	got, err := NexusStateDir(cfg)
	if err != nil {
		t.Fatalf("NexusStateDir() error = %v", err)
	}
	want := filepath.Join(cfg.AgentDockHome, "nexus")
	if got != want {
		t.Fatalf("NexusStateDir() = %q, want %q", got, want)
	}
}
