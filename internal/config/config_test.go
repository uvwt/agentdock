package config

import "testing"

func TestNormalizeDefaultsToSandboxedWorkspace(t *testing.T) {
	cfg := Config{}
	cfg.Normalize()

	if cfg.Mode != ModeSandboxed {
		t.Fatalf("Mode = %q, want %q", cfg.Mode, ModeSandboxed)
	}
	if cfg.SandboxMode != SandboxModeLandlock {
		t.Fatalf("SandboxMode = %q, want %q", cfg.SandboxMode, SandboxModeLandlock)
	}
	if cfg.PathPolicy != PathPolicyWorkspace {
		t.Fatalf("PathPolicy = %q, want %q", cfg.PathPolicy, PathPolicyWorkspace)
	}
}

func TestNormalizeHostModeSelectsNoneAndHostPathPolicy(t *testing.T) {
	cfg := Config{Mode: ModeHost}
	cfg.Normalize()

	if cfg.SandboxMode != SandboxModeNone {
		t.Fatalf("SandboxMode = %q, want %q", cfg.SandboxMode, SandboxModeNone)
	}
	if cfg.PathPolicy != PathPolicyHost {
		t.Fatalf("PathPolicy = %q, want %q", cfg.PathPolicy, PathPolicyHost)
	}
}

func TestNormalizeLegacySandboxNoneInfersHostMode(t *testing.T) {
	cfg := Config{SandboxMode: SandboxModeNone}
	cfg.Normalize()

	if cfg.Mode != ModeHost {
		t.Fatalf("Mode = %q, want %q", cfg.Mode, ModeHost)
	}
	if cfg.PathPolicy != PathPolicyHost {
		t.Fatalf("PathPolicy = %q, want %q", cfg.PathPolicy, PathPolicyHost)
	}
}
