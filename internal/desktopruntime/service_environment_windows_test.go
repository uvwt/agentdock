//go:build windows

package desktopruntime

import (
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
