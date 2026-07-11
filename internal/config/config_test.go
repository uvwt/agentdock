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

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
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

func TestFromEnvRejectsInvalidTypedValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "port", key: "AGENTDOCK_PORT", value: "not-a-number"},
		{name: "browser enabled", key: "AGENTDOCK_BROWSER_ENABLED", value: "sometimes"},
		{name: "stdio", key: "AGENTDOCK_STDIO", value: "enabled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AGENTDOCK_PORT", "")
			t.Setenv("AGENTDOCK_BROWSER_ENABLED", "")
			t.Setenv("AGENTDOCK_STDIO", "")
			t.Setenv(test.key, test.value)
			if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("FromEnv() error = %v, want %s parse error", err, test.key)
			}
		})
	}
}

func TestFromEnvParsesTypedValues(t *testing.T) {
	t.Setenv("AGENTDOCK_PORT", " 9876 ")
	t.Setenv("AGENTDOCK_BROWSER_ENABLED", "true")
	t.Setenv("AGENTDOCK_STDIO", "1")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.Port != 9876 || !cfg.BrowserEnabled || !cfg.Stdio {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestNormalizeValidatesPortAndLogLevel(t *testing.T) {
	home := t.TempDir()
	for _, test := range []struct {
		name     string
		port     int
		logLevel string
		want     string
	}{
		{name: "negative port", port: -1, logLevel: "info", want: "port must be between"},
		{name: "large port", port: 65536, logLevel: "info", want: "port must be between"},
		{name: "unknown log level", port: 8765, logLevel: "verbose", want: "unsupported log level"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := Config{
				AgentDockHome:       filepath.Join(home, test.name, "home"),
				AgentDockDefaultDir: filepath.Join(home, test.name, "workspace"),
				Port:                test.port, LogLevel: test.logLevel,
			}
			if err := cfg.Normalize(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Normalize() error = %v, want %q", err, test.want)
			}
		})
	}

	cfg := Config{
		AgentDockHome:       filepath.Join(home, "valid", "home"),
		AgentDockDefaultDir: filepath.Join(home, "valid", "workspace"),
		Port:                443, LogLevel: " WARNING ", Host: " 0.0.0.0 ",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if cfg.LogLevel != "warn" || cfg.Host != "0.0.0.0" {
		t.Fatalf("normalized config = %#v", cfg)
	}
}

func TestValidateAuthRejectsInvalidServerURL(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "password")
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "token-secret")
	for _, serverURL := range []string{
		"relative/path",
		"ftp://agentdock.example",
		"https://user:pass@agentdock.example",
		"https://agentdock.example/#fragment",
	} {
		cfg := Config{OAuthClientID: "client-id", OAuthServerURL: serverURL}
		if err := cfg.ValidateAuth(); err == nil {
			t.Fatalf("ValidateAuth() accepted %q", serverURL)
		}
	}
}
