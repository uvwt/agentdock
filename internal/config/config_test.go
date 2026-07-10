package config

import (
	"path/filepath"
	"strings"
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

func TestSkillRuntimeStateDirUsesAgentDockHome(t *testing.T) {
	home := t.TempDir()
	cfg := Config{AgentDockHome: filepath.Join(home, ".agentdock")}
	got, err := SkillRuntimeStateDir(cfg)
	if err != nil {
		t.Fatalf("SkillRuntimeStateDir() error = %v", err)
	}
	want := filepath.Join(cfg.AgentDockHome, "skill-runtime")
	if got != want {
		t.Fatalf("SkillRuntimeStateDir() = %q, want %q", got, want)
	}
}

func TestValidateAuthAllowsNoOAuthOrServerURLOnly(t *testing.T) {
	cases := []Config{
		{},
		{OAuthServerURL: "https://agentdock.example.com"},
		{AuthToken: "static-token", OAuthServerURL: "https://agentdock.example.com"},
	}
	for _, cfg := range cases {
		if err := cfg.ValidateAuth(); err != nil {
			t.Fatalf("ValidateAuth() error = %v for cfg %#v", err, cfg)
		}
		if cfg.OAuthClientID == "" && cfg.OAuthEnabled() {
			t.Fatalf("OAuthEnabled() = true without OAuthClientID")
		}
	}
}

func TestValidateAuthOAuthRequiresCompleteConfig(t *testing.T) {
	base := Config{OAuthClientID: "client-id", OAuthServerURL: "https://agentdock.example.com"}
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "password")
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-secret")
	if err := base.ValidateAuth(); err != nil {
		t.Fatalf("ValidateAuth() complete config error = %v", err)
	}

	cases := []struct {
		name    string
		cfg     Config
		unset   string
		missing string
	}{
		{name: "server url", cfg: Config{OAuthClientID: "client-id"}, missing: "AGENTDOCK_SERVER_URL"},
		{name: "password", cfg: base, unset: "AGENTDOCK_OAUTH_PASSWORD", missing: "AGENTDOCK_OAUTH_PASSWORD"},
		{name: "token secret", cfg: base, unset: "AGENTDOCK_OAUTH_TOKEN_SECRET", missing: "AGENTDOCK_OAUTH_TOKEN_SECRET"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "password")
			t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-secret")
			if tc.unset != "" {
				t.Setenv(tc.unset, "")
			}
			err := tc.cfg.ValidateAuth()
			if err == nil || !strings.Contains(err.Error(), tc.missing) {
				t.Fatalf("ValidateAuth() error = %v, want missing %s", err, tc.missing)
			}
		})
	}
}
