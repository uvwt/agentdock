//go:build windows

package desktopruntime

import (
	"encoding/json"
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

func TestLoadControlPanelSettingsMigratesLegacyACPToProfile(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, "control-panel-settings.json")
	command := filepath.Join(root, "custom-acp.exe")
	legacy, err := json.Marshal(map[string]any{
		"port": 8765, "log_level": "info", "acp_enabled": true,
		"acp_agent": "custom", "acp_command": command, "acp_args": []string{"--stdio"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := loadControlPanelSettings(root, 8765)
	if err != nil {
		t.Fatalf("loadControlPanelSettings() error = %v", err)
	}
	if settings.ACPDefaultProfile != "custom" || len(settings.ACPProfiles) != 1 {
		t.Fatalf("legacy ACP migration = default %q profiles %#v", settings.ACPDefaultProfile, settings.ACPProfiles)
	}
	profile := settings.ACPProfiles[0]
	if profile.ID != "custom" || profile.Kind != "custom" || profile.Command != filepath.Clean(command) || !profile.Enabled {
		t.Fatalf("legacy ACP profile = %#v", profile)
	}
	if len(profile.Args) != 1 || profile.Args[0] != "--stdio" {
		t.Fatalf("legacy ACP args = %#v", profile.Args)
	}
}

func TestLoadControlPanelSettingsPrefersProfilesOverLegacyACPFields(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, "control-panel-settings.json")
	command := filepath.Join(root, "zcode.exe")
	content, err := json.Marshal(map[string]any{
		"port": 8765, "log_level": "info", "acp_enabled": true,
		"acp_profiles":        []map[string]any{{"id": "zcode", "kind": "custom", "command": command, "enabled": true}},
		"acp_default_profile": "zcode", "acp_agent": "custom", "acp_command": `C:\legacy.exe`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, content, 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := loadControlPanelSettings(root, 8765)
	if err != nil {
		t.Fatalf("loadControlPanelSettings() error = %v", err)
	}
	if settings.ACPDefaultProfile != "zcode" || len(settings.ACPProfiles) != 1 || settings.ACPProfiles[0].ID != "zcode" {
		t.Fatalf("profile config did not win over legacy fields: %#v", settings)
	}
}
