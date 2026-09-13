package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFromEnvParsesACPProfile(t *testing.T) {
	t.Setenv("AGENTDOCK_ACP_ENABLED", "true")
	t.Setenv("AGENTDOCK_ACP_AGENT", "claude")
	t.Setenv("AGENTDOCK_ACP_COMMAND", filepath.Join(t.TempDir(), "agent"))
	t.Setenv("AGENTDOCK_ACP_ARGS_JSON", `["adapter.js","--flag"]`)
	t.Setenv("AGENTDOCK_ACP_ENV_FROM_ENV_JSON", `{"ANTHROPIC_API_KEY":"HOST_ANTHROPIC_KEY"}`)
	t.Setenv("AGENTDOCK_ACP_MAX_CONCURRENT_PROMPTS", "3")
	t.Setenv("AGENTDOCK_ACP_INTERACTION_TIMEOUT_MS", "45000")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ACPEnabled || cfg.ACPAgentName != "claude" || cfg.ACPMaxPrompts != 3 || cfg.ACPInteractionMS != 45000 {
		t.Fatalf("ACP config = %#v", cfg)
	}
	if !reflect.DeepEqual(cfg.ACPArgs, []string{"adapter.js", "--flag"}) {
		t.Fatalf("ACP args = %#v", cfg.ACPArgs)
	}
	if cfg.ACPEnvFromEnv["ANTHROPIC_API_KEY"] != "HOST_ANTHROPIC_KEY" {
		t.Fatalf("ACP env mapping = %#v", cfg.ACPEnvFromEnv)
	}
}

func TestNormalizeACPProfileDoesNotRequireAllowedRoots(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		AgentDockHome: t.TempDir(), AgentDockDefaultDir: t.TempDir(),
		ACPEnabled: true, ACPAgentName: "helper", ACPCommand: executable,
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if cfg.ACPMaxPrompts != 2 || cfg.ACPInteractionMS != 300000 {
		t.Fatalf("ACP defaults = prompts %d timeout %d", cfg.ACPMaxPrompts, cfg.ACPInteractionMS)
	}
}

func TestNormalizeACPRejectsUnsafeConfiguration(t *testing.T) {
	base := Config{AgentDockHome: t.TempDir(), AgentDockDefaultDir: t.TempDir(), ACPEnabled: true}
	tests := []struct {
		name   string
		want   string
		mutate func(*Config)
	}{
		{name: "invalid agent name", want: "AGENTDOCK_ACP_AGENT", mutate: func(cfg *Config) {
			cfg.ACPAgentName = "bad\nname"
			cfg.ACPCommand, _ = os.Executable()
		}},
		{name: "relative command", want: "AGENTDOCK_ACP_COMMAND", mutate: func(cfg *Config) { cfg.ACPCommand = "agent" }},
		{name: "invalid direct env mapping", want: "AGENTDOCK_ACP_ENV_FROM_ENV_JSON", mutate: func(cfg *Config) {
			cfg.ACPCommand, _ = os.Executable()
			cfg.ACPEnvFromEnv = map[string]string{"BAD-NAME": "HOST_KEY"}
		}},
		{name: "too many direct args", want: "AGENTDOCK_ACP_ARGS_JSON", mutate: func(cfg *Config) {
			cfg.ACPCommand, _ = os.Executable()
			cfg.ACPArgs = make([]string, 129)
		}},
		{name: "too many prompts", want: "AGENTDOCK_ACP_MAX_CONCURRENT_PROMPTS", mutate: func(cfg *Config) {
			cfg.ACPCommand, _ = os.Executable()
			cfg.ACPMaxPrompts = 9
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			test.mutate(&cfg)
			err := cfg.Normalize()
			if err == nil {
				t.Fatal("unsafe ACP configuration was accepted")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want it to mention %q", err, test.want)
			}
		})
	}
}

func TestFromEnvPreservesACPArgumentBytes(t *testing.T) {
	t.Setenv("AGENTDOCK_ACP_ENABLED", "true")
	t.Setenv("AGENTDOCK_ACP_ARGS_JSON", `["  spaced value  ",""]`)
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.ACPArgs, []string{"  spaced value  ", ""}) {
		t.Fatalf("ACP args were modified: %#v", cfg.ACPArgs)
	}
}

func TestFromEnvRejectsInvalidACPEnvironmentMapping(t *testing.T) {
	t.Setenv("AGENTDOCK_ACP_ENABLED", "true")
	t.Setenv("AGENTDOCK_ACP_ENV_FROM_ENV_JSON", `{"BAD-NAME":"HOST_KEY"}`)
	if _, err := FromEnv(); err == nil {
		t.Fatal("invalid ACP environment mapping was accepted")
	}
}

func TestFromEnvParsesMultipleACPProfiles(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := json.Marshal([]ACPProfile{
		{ID: "codex", Kind: "codex", Command: executable, Enabled: true},
		{ID: "zcode", Kind: "custom", Command: executable, Args: []string{"zcode.js"}, Enabled: true},
		{ID: "agy", Kind: "custom", Command: executable, Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTDOCK_ACP_ENABLED", "true")
	t.Setenv("AGENTDOCK_ACP_PROFILES_JSON", string(profiles))
	t.Setenv("AGENTDOCK_ACP_DEFAULT_PROFILE", "zcode")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AgentDockHome = t.TempDir()
	cfg.AgentDockDefaultDir = t.TempDir()
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if cfg.ACPDefaultProfile != "zcode" || cfg.ACPAgentName != "zcode" {
		t.Fatalf("default ACP profile = %q, legacy agent = %q", cfg.ACPDefaultProfile, cfg.ACPAgentName)
	}
	active := cfg.EffectiveACPProfiles()
	if len(active) != 2 || active[0].ID != "codex" || active[1].ID != "zcode" {
		t.Fatalf("active ACP profiles = %#v", active)
	}
}

func TestNormalizeACPProfilesAllowsMultipleCustomButBuiltinUsesFixedID(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		AgentDockHome: t.TempDir(), AgentDockDefaultDir: t.TempDir(), ACPEnabled: true,
		ACPProfiles: []ACPProfile{
			{ID: "codex", Kind: "codex", Command: executable, Enabled: true},
			{ID: "zcode", Kind: "custom", Command: executable, Enabled: true},
			{ID: "agy", Kind: "custom", Command: executable, Enabled: true},
		},
		ACPDefaultProfile: "zcode",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.EffectiveACPProfiles()) != 3 {
		t.Fatalf("active ACP profiles = %#v", cfg.EffectiveACPProfiles())
	}

	cfg.ACPProfiles[0].ID = "codex-work"
	if err := cfg.Normalize(); err == nil || !strings.Contains(err.Error(), "must use id") {
		t.Fatalf("renamed built-in ACP profile error = %v", err)
	}
}

func TestEffectiveACPProfilesPreservesLegacyIdentity(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		AgentDockHome: t.TempDir(), AgentDockDefaultDir: t.TempDir(),
		ACPEnabled: true, ACPAgentName: "custom", ACPCommand: executable,
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	profiles := cfg.EffectiveACPProfiles()
	if len(profiles) != 1 || profiles[0].ID != "custom" || profiles[0].Kind != "custom" {
		t.Fatalf("legacy ACP profiles = %#v", profiles)
	}
	if cfg.EffectiveACPDefaultProfile() != "custom" {
		t.Fatalf("legacy default ACP profile = %q", cfg.EffectiveACPDefaultProfile())
	}
}
