package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setTestUserHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AGENTDOCK_HOME", "")
	t.Setenv("AGENTDOCK_DEFAULT_DIR", "")
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
	if cfg.BrowserExecutablePath != "" {
		t.Fatalf("BrowserExecutablePath = %q, want empty auto-detection", cfg.BrowserExecutablePath)
	}
}

func TestFromEnvParsesCommandEnvironmentMapping(t *testing.T) {
	t.Setenv("AGENTDOCK_COMMAND_ENV_FROM_ENV_JSON", `{"NIX_LD":"NIX_LD","CHILD_TOKEN":"HOST_TOKEN"}`)

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.CommandEnvFromEnv["NIX_LD"] != "NIX_LD" || cfg.CommandEnvFromEnv["CHILD_TOKEN"] != "HOST_TOKEN" {
		t.Fatalf("CommandEnvFromEnv = %#v", cfg.CommandEnvFromEnv)
	}
}

func TestFromEnvMCPAppsEnabledDefaultsAndOverride(t *testing.T) {
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if !cfg.MCPAppsEnabled {
		t.Fatal("MCPAppsEnabled = false, want true by default")
	}

	t.Setenv("AGENTDOCK_MCP_APPS_ENABLED", "false")
	cfg, err = FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() with override error = %v", err)
	}
	if cfg.MCPAppsEnabled {
		t.Fatal("MCPAppsEnabled = true, want false from environment override")
	}
}

func TestCommandEnvironmentMappingRejectsInvalidNames(t *testing.T) {
	t.Setenv("AGENTDOCK_COMMAND_ENV_FROM_ENV_JSON", `{"BAD-NAME":"HOST_TOKEN"}`)
	if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "AGENTDOCK_COMMAND_ENV_FROM_ENV_JSON") {
		t.Fatalf("FromEnv() error = %v, want command env mapping validation error", err)
	}

	setTestUserHome(t, t.TempDir())
	cfg := Config{CommandEnvFromEnv: map[string]string{"CHILD_TOKEN": "BAD-HOST"}}
	if err := cfg.Normalize(); err == nil || !strings.Contains(err.Error(), "AGENTDOCK_COMMAND_ENV_FROM_ENV_JSON") {
		t.Fatalf("Normalize() error = %v, want command env mapping validation error", err)
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

func TestFromEnvIgnoresRemovedNexusCredentials(t *testing.T) {
	t.Setenv("AGENTDOCK_NEXUS_ENDPOINT", "https://legacy-nexus.example.com")
	t.Setenv("AGENTDOCK_NEXUS_TOKEN", "legacy-secret")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.NexusEndpoint != "" || cfg.NexusDeviceToken != "" {
		t.Fatalf("legacy Nexus credentials were loaded: %#v", cfg)
	}
}

func TestFromEnvUsesExplicitAgentDockDirectories(t *testing.T) {
	profile := t.TempDir()
	setTestUserHome(t, profile)
	home := filepath.Join(t.TempDir(), "state")
	workspace := filepath.Join(t.TempDir(), "workspace")
	t.Setenv("AGENTDOCK_HOME", home)
	t.Setenv("AGENTDOCK_DEFAULT_DIR", workspace)

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if cfg.AgentDockHome != home {
		t.Fatalf("AgentDockHome = %q, want %q", cfg.AgentDockHome, home)
	}
	if cfg.AgentDockDefaultDir != workspace {
		t.Fatalf("AgentDockDefaultDir = %q, want %q", cfg.AgentDockDefaultDir, workspace)
	}
}

func TestFromEnvRejectsRelativeAgentDockDirectories(t *testing.T) {
	setTestUserHome(t, t.TempDir())
	for _, test := range []struct {
		name string
		key  string
		want string
	}{
		{name: "home", key: "AGENTDOCK_HOME", want: "AgentDockHome"},
		{name: "default directory", key: "AGENTDOCK_DEFAULT_DIR", want: "AgentDockDefaultDir"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AGENTDOCK_HOME", "")
			t.Setenv("AGENTDOCK_DEFAULT_DIR", "")
			t.Setenv(test.key, "relative/path")
			cfg, err := FromEnv()
			if err != nil {
				t.Fatalf("FromEnv() error = %v", err)
			}
			err = cfg.Normalize()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Normalize() error = %v, want %q", err, test.want)
			}
		})
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
		{name: "browser reuse existing cdp", key: "AGENTDOCK_BROWSER_REUSE_EXISTING_CDP", value: "sometimes"},
		{name: "oauth enabled", key: "AGENTDOCK_OAUTH_ENABLED", value: "enabled"},
		{name: "oauth access token ttl", key: "AGENTDOCK_OAUTH_ACCESS_TOKEN_TTL", value: "one-day"},
		{name: "stdio", key: "AGENTDOCK_STDIO", value: "enabled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("AGENTDOCK_PORT", "")
			t.Setenv("AGENTDOCK_BROWSER_ENABLED", "")
			t.Setenv("AGENTDOCK_BROWSER_REUSE_EXISTING_CDP", "")
			t.Setenv("AGENTDOCK_OAUTH_ENABLED", "")
			t.Setenv("AGENTDOCK_OAUTH_ACCESS_TOKEN_TTL", "")
			t.Setenv("AGENTDOCK_STDIO", "")
			t.Setenv(test.key, test.value)
			if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("FromEnv() error = %v, want %s parse error", err, test.key)
			}
		})
	}
}

func TestFromEnvParsesTypedValues(t *testing.T) {
	tempDir := t.TempDir()
	browserPath := filepath.Join(tempDir, "chromium")
	t.Setenv("AGENTDOCK_PORT", " 9876 ")
	t.Setenv("AGENTDOCK_BROWSER_ENABLED", "true")
	t.Setenv("AGENTDOCK_BROWSER_EXECUTABLE_PATH", browserPath)
	t.Setenv("AGENTDOCK_BROWSER_CDP_URL", "http://127.0.0.1:9222")
	t.Setenv("AGENTDOCK_BROWSER_REUSE_EXISTING_CDP", "true")
	t.Setenv("AGENTDOCK_OAUTH_ENABLED", "true")
	t.Setenv("AGENTDOCK_OAUTH_ACCESS_TOKEN_TTL", "24h")
	t.Setenv("AGENTDOCK_STDIO", "1")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.Port != 9876 || !cfg.BrowserEnabled || !cfg.OAuthEnabled || !cfg.Stdio || cfg.OAuthAccessTokenTTLSeconds != int64(24*time.Hour/time.Second) ||
		cfg.BrowserExecutablePath != browserPath || cfg.BrowserCDPURL != "http://127.0.0.1:9222" || !cfg.BrowserReuseExistingCDP {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestFromEnvParsesLongOAuthAccessTokenTTLInDays(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_ACCESS_TOKEN_TTL", "999999d")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	want := int64(999999 * 24 * 60 * 60)
	if cfg.OAuthAccessTokenTTLSeconds != want {
		t.Fatalf("OAuthAccessTokenTTLSeconds = %d, want %d", cfg.OAuthAccessTokenTTLSeconds, want)
	}
}

func TestFromEnvParsesNeverExpiringOAuthAccessToken(t *testing.T) {
	t.Setenv("AGENTDOCK_OAUTH_ACCESS_TOKEN_TTL", "never")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if !cfg.OAuthAccessTokenNeverExpires || cfg.OAuthAccessTokenTTLSeconds != 0 {
		t.Fatalf("config = %#v, want a non-expiring OAuth access token", cfg)
	}
}

func TestValidateOAuthAccessTokenTTL(t *testing.T) {
	for _, value := range []string{"1m", "24h", "30d", "999999d", "never"} {
		if err := ValidateOAuthAccessTokenTTL(value); err != nil {
			t.Fatalf("ValidateOAuthAccessTokenTTL(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{"", "59s", "1000000d", "1.5s", "invalid"} {
		if err := ValidateOAuthAccessTokenTTL(value); err == nil {
			t.Fatalf("ValidateOAuthAccessTokenTTL(%q) accepted invalid value", value)
		}
	}
}

func TestNormalizeValidatesOAuthAccessTokenTTL(t *testing.T) {
	home := t.TempDir()
	for _, test := range []struct {
		name       string
		ttlSeconds int64
	}{
		{name: "too short", ttlSeconds: 1},
		{name: "too long", ttlSeconds: int64(999999*24*60*60) + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := Config{
				AgentDockHome:              filepath.Join(home, test.name, "home"),
				AgentDockDefaultDir:        filepath.Join(home, test.name, "workspace"),
				OAuthAccessTokenTTLSeconds: test.ttlSeconds,
			}
			if err := cfg.Normalize(); err == nil || !strings.Contains(err.Error(), "AGENTDOCK_OAUTH_ACCESS_TOKEN_TTL") {
				t.Fatalf("Normalize() error = %v, want OAuth access token TTL error", err)
			}
		})
	}
}

func TestNormalizeRejectsRelativeBrowserExecutablePath(t *testing.T) {
	home := t.TempDir()
	setTestUserHome(t, home)
	cfg := Config{BrowserExecutablePath: "relative/chromium"}
	if err := cfg.Normalize(); err == nil || !strings.Contains(err.Error(), "BrowserExecutablePath must resolve to an absolute path") {
		t.Fatalf("Normalize() error = %v, want absolute BrowserExecutablePath error", err)
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
