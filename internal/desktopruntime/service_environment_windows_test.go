//go:build windows

package desktopruntime

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestEffectiveOAuthAccessTokenTTL(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		inherited  string
		want       string
	}{
		{name: "persisted wins", configured: "30d", inherited: "1h", want: "30d"},
		{name: "env fallback", inherited: "24h", want: "24h"},
		{name: "core default", want: ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := effectiveOAuthAccessTokenTTL(test.configured, test.inherited); got != test.want {
				t.Fatalf("effectiveOAuthAccessTokenTTL(%q, %q) = %q, want %q", test.configured, test.inherited, got, test.want)
			}
		})
	}
}

func TestLoadControlPanelSettingsValidatesOAuthAccessTokenTTL(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, "control-panel-settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"port":8765,"log_level":"info","oauth_access_token_ttl":"never"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := loadControlPanelSettings(root, 8765)
	if err != nil {
		t.Fatalf("loadControlPanelSettings() error = %v", err)
	}
	if settings.OAuthAccessTokenTTL != "never" {
		t.Fatalf("OAuthAccessTokenTTL = %q, want never", settings.OAuthAccessTokenTTL)
	}
	if !settings.MCPAppsEnabled {
		t.Fatal("legacy settings without mcp_apps_enabled should default MCP Apps UI to enabled")
	}

	if err := os.WriteFile(settingsPath, []byte(`{"port":8765,"log_level":"info","oauth_access_token_ttl":"59s"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadControlPanelSettings(root, 8765); err == nil {
		t.Fatal("loadControlPanelSettings() accepted invalid OAuth access token TTL")
	}
}

func TestPlatformPrepareCoreEnvironmentRecoversCorruptGeneratedCredentialsConsistently(t *testing.T) {
	root := t.TempDir()
	for _, name := range append(append([]string(nil), managedCoreEnvironment...), "AGENTDOCK_RUNTIME_ROOT") {
		t.Setenv(name, os.Getenv(name))
	}

	manifest := Manifest{
		SchemaVersion:   SchemaVersion,
		InstallRoot:     root,
		AgentDockBinary: filepath.Join(root, "bin", "agentdock.exe"),
		Host:            "127.0.0.1",
		Port:            18765,
		LocalMCPURL:     "http://127.0.0.1:18765/mcp",
		TunnelMode:      "none",
		InstallChannel:  "test",
	}
	if err := Save(filepath.Join(root, "runtime.json"), manifest); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "server-url.txt"), []byte("https://agentdock.example"), 0o600); err != nil {
		t.Fatal(err)
	}

	garbage := base64.StdEncoding.EncodeToString([]byte("garbage-not-a-dpapi-blob"))
	credentials := []struct {
		path    string
		entropy string
		envName string
		length  int
	}{
		{path: "auth-token.dpapi", entropy: "agentdock.startup.v1", envName: "AGENTDOCK_AUTH_TOKEN", length: 64},
		{path: "oauth-password.dpapi", entropy: "agentdock.oauth.password.v1", envName: "AGENTDOCK_OAUTH_PASSWORD", length: 24},
		{path: "oauth-token-secret.dpapi", entropy: "agentdock.oauth.secret.v1", envName: "AGENTDOCK_OAUTH_TOKEN_SECRET", length: 64},
	}
	for _, credential := range credentials {
		if err := os.WriteFile(filepath.Join(root, credential.path), []byte(garbage), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := platformPrepareCoreEnvironment(root); err != nil {
		t.Fatalf("platformPrepareCoreEnvironment() error = %v", err)
	}
	for _, credential := range credentials {
		persisted, err := readProtectedText(filepath.Join(root, credential.path), credential.entropy)
		if err != nil {
			t.Fatalf("readProtectedText(%s) error = %v", credential.path, err)
		}
		if len(persisted) != credential.length {
			t.Fatalf("%s plaintext length = %d, want %d", credential.path, len(persisted), credential.length)
		}
		if runtimeValue := os.Getenv(credential.envName); runtimeValue != persisted {
			t.Fatalf("%s runtime value differs from persisted DPAPI value", credential.envName)
		}
		backups, err := filepath.Glob(filepath.Join(root, credential.path+".unreadable-*.bak"))
		if err != nil {
			t.Fatal(err)
		}
		if len(backups) != 1 {
			t.Fatalf("%s unreadable backup count = %d, want 1", credential.path, len(backups))
		}
		backupData, err := os.ReadFile(backups[0])
		if err != nil {
			t.Fatal(err)
		}
		if string(backupData) != garbage {
			t.Fatalf("%s unreadable backup did not preserve original ciphertext", credential.path)
		}
	}
	if got := os.Getenv("AGENTDOCK_OAUTH_ENABLED"); got != "true" {
		t.Fatalf("AGENTDOCK_OAUTH_ENABLED = %q, want true", got)
	}
	if got := os.Getenv("AGENTDOCK_SERVER_URL"); got != "https://agentdock.example" {
		t.Fatalf("AGENTDOCK_SERVER_URL = %q", got)
	}
}
