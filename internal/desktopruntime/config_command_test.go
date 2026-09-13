package desktopruntime

import (
	"os"
	"testing"

	agentconfig "github.com/uvwt/agentdock/internal/config"
)

func TestValidateConfigUpdate(t *testing.T) {
	valid := ConfigUpdateRequest{RuntimeRoot: "runtime", Port: 8765, LogLevel: "info"}
	if err := validateConfigUpdate(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	validCDP := valid
	validCDP.BrowserCDPURL = "http://browser.internal:9222"
	if err := validateConfigUpdate(validCDP); err != nil {
		t.Fatalf("valid CDP config rejected: %v", err)
	}

	validBuiltinACP := valid
	validBuiltinACP.ACPEnabled = true
	validBuiltinACP.ACPDefaultProfile = "grok"
	validBuiltinACP.ACPProfiles = []agentconfig.ACPProfile{{ID: "grok", Kind: "grok", Enabled: true}}
	if err := validateConfigUpdate(validBuiltinACP); err != nil {
		t.Fatalf("valid builtin ACP config rejected: %v", err)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	validProfiles := valid
	validProfiles.ACPEnabled = true
	validProfiles.ACPDefaultProfile = "zcode"
	validProfiles.ACPProfiles = []agentconfig.ACPProfile{
		{ID: "codex", Kind: "codex", Command: executable, Enabled: true},
		{ID: "zcode", Kind: "custom", Command: executable, Enabled: true},
		{ID: "agy", Kind: "custom", Command: executable, Enabled: true},
	}
	if err := validateConfigUpdate(validProfiles); err != nil {
		t.Fatalf("valid ACP profiles rejected: %v", err)
	}

	invalidBuiltinProfile := validProfiles
	invalidBuiltinProfile.ACPProfiles = append([]agentconfig.ACPProfile(nil), validProfiles.ACPProfiles...)
	invalidBuiltinProfile.ACPProfiles[0].ID = "codex-work"
	if err := validateConfigUpdate(invalidBuiltinProfile); err == nil {
		t.Fatal("renamed built-in ACP profile was accepted")
	}

	invalidDefaultProfile := validProfiles
	invalidDefaultProfile.ACPDefaultProfile = "missing"
	if err := validateConfigUpdate(invalidDefaultProfile); err == nil {
		t.Fatal("missing default ACP profile was accepted")
	}

	invalidProfileID := validProfiles
	invalidProfileID.ACPProfiles = append([]agentconfig.ACPProfile(nil), validProfiles.ACPProfiles...)
	invalidProfileID.ACPProfiles[1].ID = "中文"
	if err := validateConfigUpdate(invalidProfileID); err == nil {
		t.Fatal("non-ASCII ACP profile id was accepted")
	}

	validTTL := valid
	validTTL.OAuthAccessTokenTTL = "30d"
	if err := validateConfigUpdate(validTTL); err != nil {
		t.Fatalf("valid OAuth access token TTL rejected: %v", err)
	}
	validNeverTTL := valid
	validNeverTTL.OAuthAccessTokenTTL = "never"
	if err := validateConfigUpdate(validNeverTTL); err != nil {
		t.Fatalf("valid never-expiring OAuth access token TTL rejected: %v", err)
	}

	cases := []ConfigUpdateRequest{
		{Port: 8765, LogLevel: "info"},
		{RuntimeRoot: "runtime", Port: 0, LogLevel: "info"},
		{RuntimeRoot: "runtime", Port: 8765, LogLevel: "verbose"},
		{RuntimeRoot: "runtime", Port: 8765, LogLevel: "info", BrowserCDPURL: "file:///tmp/cdp"},
		{RuntimeRoot: "runtime", Port: 8765, LogLevel: "info", BrowserCDPURL: "http://user:pass@browser.internal:9222"},
		{RuntimeRoot: "runtime", Port: 8765, LogLevel: "info", BrowserCDPURL: "http://browser.internal:9222/#fragment"},
		{RuntimeRoot: "runtime", Port: 8765, LogLevel: "info", ACPEnabled: true},
		{RuntimeRoot: "runtime", Port: 8765, LogLevel: "info", ACPEnabled: true, ACPProfiles: []agentconfig.ACPProfile{{ID: "other", Kind: "other", Enabled: true}}},
		{RuntimeRoot: "runtime", Port: 8765, LogLevel: "info", ACPEnabled: true, ACPProfiles: []agentconfig.ACPProfile{{ID: "custom", Kind: "custom", Enabled: true}}, ACPDefaultProfile: "custom"},
		{RuntimeRoot: "runtime", Port: 8765, LogLevel: "info", OAuthAccessTokenTTL: "59s"},
		{RuntimeRoot: "runtime", Port: 8765, LogLevel: "info", OAuthAccessTokenTTL: "1000000d"},
	}
	for _, request := range cases {
		if err := validateConfigUpdate(request); err == nil {
			t.Fatalf("invalid config accepted: %#v", request)
		}
	}
}
