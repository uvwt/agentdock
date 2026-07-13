package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func setTestUserHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestNormalizeDefaultsToUserDirectories(t *testing.T) {
	home := t.TempDir()
	setTestUserHome(t, home)
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
	setTestUserHome(t, home)
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

func TestSkillStateDirUsesAgentDockHome(t *testing.T) {
	home := t.TempDir()
	cfg := Config{AgentDockHome: filepath.Join(home, ".agentdock")}
	got, err := SkillStateDir(cfg)
	if err != nil {
		t.Fatalf("SkillStateDir() error = %v", err)
	}
	want := filepath.Join(cfg.AgentDockHome, "skill-store")
	if got != want {
		t.Fatalf("SkillStateDir() = %q, want %q", got, want)
	}
}

func TestValidateAuthAllowsNoOAuthOrServerURLOnly(t *testing.T) {
	cases := []Config{
		{Host: "127.0.0.1"},
		{Host: "127.0.0.1", OAuthServerURL: "https://agentdock.example.com"},
		{Host: "0.0.0.0", AuthToken: "static-token", OAuthServerURL: "https://agentdock.example.com"},
	}
	for _, cfg := range cases {
		if err := cfg.ValidateAuth(); err != nil {
			t.Fatalf("ValidateAuth() error = %v for cfg %#v", err, cfg)
		}
		if cfg.OAuthEnabled {
			t.Fatalf("OAuthEnabled = true without the explicit enable flag")
		}
	}
}

func TestValidateAuthOAuthRequiresCompleteConfig(t *testing.T) {
	base := Config{OAuthEnabled: true, OAuthServerURL: "https://agentdock.example.com"}
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "strong-password")
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "0123456789abcdef0123456789abcdef")
	if err := base.ValidateAuth(); err != nil {
		t.Fatalf("ValidateAuth() complete config error = %v", err)
	}

	cases := []struct {
		name    string
		cfg     Config
		unset   string
		missing string
	}{
		{name: "server url", cfg: Config{OAuthEnabled: true}, missing: "AGENTDOCK_SERVER_URL"},
		{name: "password", cfg: base, unset: "AGENTDOCK_OAUTH_PASSWORD", missing: "AGENTDOCK_OAUTH_PASSWORD"},
		{name: "token secret", cfg: base, unset: "AGENTDOCK_OAUTH_TOKEN_SECRET", missing: "AGENTDOCK_OAUTH_TOKEN_SECRET"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "strong-password")
			t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "0123456789abcdef0123456789abcdef")
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

func TestValidateAuthRejectsWeakOAuthCredentials(t *testing.T) {
	cfg := Config{OAuthEnabled: true, OAuthServerURL: "https://agentdock.example.com"}
	for name, credentials := range map[string][2]string{
		"short password": {"short", "0123456789abcdef0123456789abcdef"},
		"short secret":   {"strong-password", "short"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("AGENTDOCK_OAUTH_PASSWORD", credentials[0])
			t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", credentials[1])
			if err := cfg.ValidateAuth(); err == nil {
				t.Fatal("ValidateAuth() accepted weak OAuth credentials")
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
		{name: "oauth enabled", key: "AGENTDOCK_OAUTH_ENABLED", value: "enabled"},
		{name: "stdio", key: "AGENTDOCK_STDIO", value: "enabled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AGENTDOCK_PORT", "")
			t.Setenv("AGENTDOCK_BROWSER_ENABLED", "")
			t.Setenv("AGENTDOCK_OAUTH_ENABLED", "")
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
	t.Setenv("AGENTDOCK_OAUTH_ENABLED", "true")
	t.Setenv("AGENTDOCK_STDIO", "1")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.Port != 9876 || !cfg.BrowserEnabled || !cfg.OAuthEnabled || !cfg.Stdio {
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
		Port:                443,
		LogLevel:            " WARNING ",
		Host:                " 0.0.0.0 ",
		OAuthServerURL:      " https://agentdock.example.com ",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if cfg.LogLevel != "warn" || cfg.Host != "0.0.0.0" || cfg.OAuthServerURL != "https://agentdock.example.com" {
		t.Fatalf("normalized config = %#v", cfg)
	}
}

func TestValidateAuthRejectsInvalidServerURL(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "strong-password")
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "0123456789abcdef0123456789abcdef")
	for _, serverURL := range []string{
		"relative/path",
		"ftp://agentdock.example",
		"http://agentdock.example",
		"https://user:pass@agentdock.example",
		"https://agentdock.example/base",
		"https://agentdock.example?mode=test",
		"https://agentdock.example/#fragment",
	} {
		cfg := Config{OAuthEnabled: true, OAuthServerURL: serverURL}
		if err := cfg.ValidateAuth(); err == nil {
			t.Fatalf("ValidateAuth() accepted %q", serverURL)
		}
	}
}

func TestValidateAuthAllowsHTTPOnlyForLoopbackHosts(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_PASSWORD", "strong-password")
	t.Setenv("AGENTDOCK_OAUTH_TOKEN_SECRET", "0123456789abcdef0123456789abcdef")
	for _, serverURL := range []string{
		"http://localhost:8765",
		"http://127.0.0.1:8765",
		"http://[::1]:8765",
	} {
		cfg := Config{OAuthEnabled: true, OAuthServerURL: serverURL}
		if err := cfg.ValidateAuth(); err != nil {
			t.Fatalf("ValidateAuth() rejected loopback URL %q: %v", serverURL, err)
		}
	}
}

func TestValidateAuthRejectsUnauthenticatedNonLoopbackListener(t *testing.T) {
	for _, host := range []string{"0.0.0.0", "::", "192.0.2.10", "agentdock.internal"} {
		cfg := Config{Host: host}
		if err := cfg.ValidateAuth(); err == nil || !strings.Contains(err.Error(), "requires AGENTDOCK_AUTH_TOKEN or OAuth") {
			t.Fatalf("ValidateAuth(%q) error = %v, want non-loopback authentication error", host, err)
		}
	}
}

func TestValidateAuthAllowsSafeListenerModes(t *testing.T) {
	for _, cfg := range []Config{
		{Host: "127.0.0.1"},
		{Host: "::1"},
		{Host: "localhost"},
		{Host: "0.0.0.0", AuthToken: "configured-token"},
		{Host: "0.0.0.0", Stdio: true},
	} {
		if err := cfg.ValidateAuth(); err != nil {
			t.Fatalf("ValidateAuth(%#v) error = %v", cfg, err)
		}
	}
}

func TestNormalizeValidatesAndCanonicalizesTrustedProxyCIDRs(t *testing.T) {
	home := t.TempDir()
	cfg := Config{
		AgentDockHome:       filepath.Join(home, "home"),
		AgentDockDefaultDir: filepath.Join(home, "workspace"),
		TrustedProxyCIDRs:   []string{"127.0.0.1/8", "127.0.0.0/8", "::1/128"},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedProxyCIDRs) != 2 || cfg.TrustedProxyCIDRs[0] != "127.0.0.0/8" || cfg.TrustedProxyCIDRs[1] != "::1/128" {
		t.Fatalf("TrustedProxyCIDRs = %#v", cfg.TrustedProxyCIDRs)
	}
	cfg.TrustedProxyCIDRs = []string{"not-a-cidr"}
	if err := cfg.Normalize(); err == nil || !strings.Contains(err.Error(), "invalid CIDR") {
		t.Fatalf("Normalize() error = %v, want invalid CIDR", err)
	}
}

func TestFromEnvReadsTrustedProxyCIDRs(t *testing.T) {
	home := t.TempDir()
	setTestUserHome(t, home)
	t.Setenv("AGENTDOCK_TRUSTED_PROXY_CIDRS", "127.0.0.0/8, ::1/128")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedProxyCIDRs) != 2 {
		t.Fatalf("TrustedProxyCIDRs = %#v", cfg.TrustedProxyCIDRs)
	}
}

func TestValidateAuthTreatsEmptyHostAsWildcard(t *testing.T) {
	cfg := Config{Host: ""}
	if err := cfg.ValidateAuth(); err == nil || !strings.Contains(err.Error(), "requires AGENTDOCK_AUTH_TOKEN or OAuth") {
		t.Fatalf("ValidateAuth() error = %v, want wildcard authentication error", err)
	}
}
