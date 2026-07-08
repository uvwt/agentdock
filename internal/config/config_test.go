package config

import (
	"os"
	"testing"
)

func TestNormalizeDefaultsToWorkspaceRuntimeProfile(t *testing.T) {
	cfg := Config{}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	if cfg.RuntimeProfile != RuntimeProfileWorkspace {
		t.Fatalf("RuntimeProfile = %q, want %q", cfg.RuntimeProfile, RuntimeProfileWorkspace)
	}
	if cfg.HostPaths() {
		t.Fatal("workspace profile should not allow host paths")
	}
	if !cfg.CommandSandboxEnabled() {
		t.Fatal("workspace profile should enable command sandbox")
	}
	if cfg.PathPolicyName() != "workspace" {
		t.Fatalf("PathPolicyName() = %q, want workspace", cfg.PathPolicyName())
	}
	if cfg.CommandSandboxName() != "landlock" {
		t.Fatalf("CommandSandboxName() = %q, want landlock", cfg.CommandSandboxName())
	}
}

func TestRuntimeProfileHostDerivesHostPathsAndNoCommandSandbox(t *testing.T) {
	cfg := Config{RuntimeProfile: RuntimeProfileHost}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	if !cfg.HostPaths() {
		t.Fatal("host profile should allow host paths")
	}
	if cfg.CommandSandboxEnabled() {
		t.Fatal("host profile should disable command sandbox")
	}
	if cfg.PathPolicyName() != "host" {
		t.Fatalf("PathPolicyName() = %q, want host", cfg.PathPolicyName())
	}
	if cfg.CommandSandboxName() != "none" {
		t.Fatalf("CommandSandboxName() = %q, want none", cfg.CommandSandboxName())
	}
}

func TestNormalizeRejectsInvalidRuntimeProfile(t *testing.T) {
	cfg := Config{RuntimeProfile: "sandboxed"}
	if err := cfg.Normalize(); err == nil {
		t.Fatal("Normalize() should reject invalid runtime profile")
	}
}

func TestFromEnvReadsRuntimeProfile(t *testing.T) {
	t.Setenv("AGENTDOCK_RUNTIME_PROFILE", RuntimeProfileHost)

	cfg := FromEnv()
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if cfg.RuntimeProfile != RuntimeProfileHost {
		t.Fatalf("RuntimeProfile = %q, want %q", cfg.RuntimeProfile, RuntimeProfileHost)
	}
	if cfg.PathPolicyName() != "host" || cfg.CommandSandboxName() != "none" {
		t.Fatalf("derived profile = path %q sandbox %q, want host/none", cfg.PathPolicyName(), cfg.CommandSandboxName())
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
