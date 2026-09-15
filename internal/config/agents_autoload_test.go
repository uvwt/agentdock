package config

import "testing"

func TestAgentsAutoLoadEnvironment(t *testing.T) {
	for _, test := range []struct {
		value    string
		disabled bool
		invalid  bool
	}{
		{value: ""},
		{value: "true"},
		{value: "1"},
		{value: "false", disabled: true},
		{value: "0", disabled: true},
		{value: "unexpected", invalid: true},
	} {
		t.Run("value="+test.value, func(t *testing.T) {
			t.Setenv("AGENTDOCK_AGENTS_AUTOLOAD", test.value)
			t.Setenv("AGENTDOCK_ACP_ENABLED", "false")
			cfg, err := FromEnv()
			if test.invalid {
				if err == nil {
					t.Fatal("accepted invalid autoload boolean")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.AgentsAutoLoadDisabled != test.disabled {
				t.Fatalf("disabled=%v, want %v", cfg.AgentsAutoLoadDisabled, test.disabled)
			}
		})
	}
}
