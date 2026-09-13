package config

import "testing"

func TestDisabledACPIgnoresResidualEnvironment(t *testing.T) {
	t.Setenv("AGENTDOCK_ACP_ENABLED", "false")
	t.Setenv("AGENTDOCK_ACP_AGENT", "bad\nname")
	t.Setenv("AGENTDOCK_ACP_COMMAND", "relative-adapter")
	t.Setenv("AGENTDOCK_ACP_ARGS_JSON", `not-json`)
	t.Setenv("AGENTDOCK_ACP_ENV_FROM_ENV_JSON", `{"BAD-NAME":"HOST_KEY"}`)
	t.Setenv("AGENTDOCK_ACP_MAX_CONCURRENT_PROMPTS", "not-an-integer")
	t.Setenv("AGENTDOCK_ACP_INTERACTION_TIMEOUT_MS", "not-an-integer")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("disabled ACP parsed residual configuration: %v", err)
	}
	setTestUserHome(t, t.TempDir())
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("disabled ACP normalized residual configuration: %v", err)
	}
	assertDisabledACPDefaults(t, cfg)
}

func TestNormalizeDisabledACPClearsProgrammaticResiduals(t *testing.T) {
	setTestUserHome(t, t.TempDir())
	cfg := Config{
		ACPProfiles:       []ACPProfile{{ID: "stale", Kind: "custom", Command: "relative", Enabled: true}},
		ACPDefaultProfile: "stale", ACPMaxPrompts: 99, ACPInteractionMS: -1,
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("disabled programmatic ACP config failed: %v", err)
	}
	assertDisabledACPDefaults(t, cfg)
}

func assertDisabledACPDefaults(t *testing.T, cfg Config) {
	t.Helper()
	if cfg.ACPEnabled || cfg.ACPProfiles != nil || cfg.ACPDefaultProfile != "" ||
		cfg.ACPMaxPrompts != 2 || cfg.ACPInteractionMS != 300000 {
		t.Fatalf("disabled ACP defaults = %#v", cfg)
	}
}
