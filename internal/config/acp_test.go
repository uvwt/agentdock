package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFromEnvMigratesLegacyACPToProfile(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTDOCK_ACP_ENABLED", "true")
	t.Setenv("AGENTDOCK_ACP_AGENT", "custom")
	t.Setenv("AGENTDOCK_ACP_COMMAND", executable)
	t.Setenv("AGENTDOCK_ACP_ARGS_JSON", `["adapter.js","--flag"]`)
	t.Setenv("AGENTDOCK_ACP_ENV_FROM_ENV_JSON", `{"API_KEY":"HOST_API_KEY"}`)
	t.Setenv("AGENTDOCK_ACP_MAX_CONCURRENT_PROMPTS", "3")
	t.Setenv("AGENTDOCK_ACP_INTERACTION_TIMEOUT_MS", "45000")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ACPEnabled || cfg.ACPDefaultProfile != "custom" || cfg.ACPMaxPrompts != 3 || cfg.ACPInteractionMS != 45000 {
		t.Fatalf("ACP config = %#v", cfg)
	}
	if len(cfg.ACPProfiles) != 1 {
		t.Fatalf("ACP profiles = %#v", cfg.ACPProfiles)
	}
	profile := cfg.ACPProfiles[0]
	if profile.ID != "custom" || profile.Kind != "custom" || profile.Command != executable || !profile.Enabled {
		t.Fatalf("legacy ACP profile = %#v", profile)
	}
	if !reflect.DeepEqual(profile.Args, []string{"adapter.js", "--flag"}) {
		t.Fatalf("ACP args = %#v", profile.Args)
	}
	if profile.EnvFromEnv["API_KEY"] != "HOST_API_KEY" {
		t.Fatalf("ACP env mapping = %#v", profile.EnvFromEnv)
	}
}

func TestNormalizeACPProfileDoesNotRequireAllowedRoots(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		AgentDockHome: t.TempDir(), AgentDockDefaultDir: t.TempDir(), ACPEnabled: true,
		ACPProfiles:       []ACPProfile{{ID: "helper", Kind: "custom", Command: executable, Enabled: true}},
		ACPDefaultProfile: "helper",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if cfg.ACPMaxPrompts != 2 || cfg.ACPInteractionMS != 300000 {
		t.Fatalf("ACP defaults = prompts %d timeout %d", cfg.ACPMaxPrompts, cfg.ACPInteractionMS)
	}
}

func TestNormalizeACPRejectsUnsafeConfiguration(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base := Config{
		AgentDockHome: t.TempDir(), AgentDockDefaultDir: t.TempDir(), ACPEnabled: true,
		ACPProfiles:       []ACPProfile{{ID: "helper", Kind: "custom", Command: executable, Enabled: true}},
		ACPDefaultProfile: "helper",
	}
	tests := []struct {
		name   string
		want   string
		mutate func(*Config)
	}{
		{name: "invalid profile id", want: "profile id", mutate: func(cfg *Config) { cfg.ACPProfiles[0].ID = "bad\nname" }},
		{name: "relative command", want: "absolute executable path", mutate: func(cfg *Config) { cfg.ACPProfiles[0].Command = "agent" }},
		{name: "invalid env mapping", want: "env_from_env", mutate: func(cfg *Config) {
			cfg.ACPProfiles[0].EnvFromEnv = map[string]string{"BAD-NAME": "HOST_KEY"}
		}},
		{name: "too many args", want: "args", mutate: func(cfg *Config) { cfg.ACPProfiles[0].Args = make([]string, 129) }},
		{name: "too many prompts", want: "AGENTDOCK_ACP_MAX_CONCURRENT_PROMPTS", mutate: func(cfg *Config) { cfg.ACPMaxPrompts = 9 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			cfg.ACPProfiles = append([]ACPProfile(nil), base.ACPProfiles...)
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

func TestFromEnvPreservesLegacyACPArgumentBytes(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTDOCK_ACP_ENABLED", "true")
	t.Setenv("AGENTDOCK_ACP_AGENT", "custom")
	t.Setenv("AGENTDOCK_ACP_COMMAND", executable)
	t.Setenv("AGENTDOCK_ACP_ARGS_JSON", `["  spaced value  ",""]`)
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ACPProfiles) != 1 || !reflect.DeepEqual(cfg.ACPProfiles[0].Args, []string{"  spaced value  ", ""}) {
		t.Fatalf("ACP args were modified: %#v", cfg.ACPProfiles)
	}
}

func TestFromEnvRejectsInvalidLegacyACPEnvironmentMapping(t *testing.T) {
	t.Setenv("AGENTDOCK_ACP_ENABLED", "true")
	t.Setenv("AGENTDOCK_ACP_ENV_FROM_ENV_JSON", `{"BAD-NAME":"HOST_KEY"}`)
	if _, err := FromEnv(); err == nil {
		t.Fatal("invalid legacy ACP environment mapping was accepted")
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
	if cfg.ACPDefaultProfile != "zcode" {
		t.Fatalf("default ACP profile = %q", cfg.ACPDefaultProfile)
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

func TestLegacyCustomMigrationPreservesSessionIdentity(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTDOCK_ACP_ENABLED", "true")
	t.Setenv("AGENTDOCK_ACP_AGENT", "custom")
	t.Setenv("AGENTDOCK_ACP_COMMAND", executable)

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ACPProfiles) != 1 || cfg.ACPProfiles[0].ID != "custom" || cfg.ACPProfiles[0].Kind != "custom" {
		t.Fatalf("legacy custom migration = %#v", cfg.ACPProfiles)
	}
	if cfg.ACPDefaultProfile != "custom" {
		t.Fatalf("legacy default ACP profile = %q", cfg.ACPDefaultProfile)
	}
}

func TestLegacyACPProfileUsesBuiltinKindForBuiltinAgent(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTDOCK_ACP_ENABLED", "true")
	t.Setenv("AGENTDOCK_ACP_AGENT", "claude")
	t.Setenv("AGENTDOCK_ACP_COMMAND", filepath.Clean(executable))

	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ACPProfiles) != 1 || cfg.ACPProfiles[0].ID != "claude" || cfg.ACPProfiles[0].Kind != "claude" {
		t.Fatalf("legacy builtin migration = %#v", cfg.ACPProfiles)
	}
}
