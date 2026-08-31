package command

import (
	"os"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
)

func TestCommandEnvExplicitPathOverridesPlatformDefault(t *testing.T) {
	home := t.TempDir()
	envs, err := envstore.New(home)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(
		func() config.Config { return config.Config{AgentDockHome: home, AgentDockDefaultDir: home} },
		nil,
		envs,
		nil,
		nil,
	)

	t.Setenv("PATH", "/usr/bin:/bin")
	got, err := svc.CommandEnv("", map[string]string{"PATH": "/custom/bin"})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range got {
		if strings.HasPrefix(entry, "PATH=") {
			if entry != "PATH=/custom/bin" {
				t.Fatalf("PATH override = %q, want %q", entry, "PATH=/custom/bin")
			}
			return
		}
	}
	t.Fatal("command environment missing PATH")
}

func TestCommandEnvMappingOverridesBaseEnvironment(t *testing.T) {
	svc, cfg := newCommandTestService(t)
	t.Setenv("AGENTDOCK_TEST_MAPPED_PATH", "/mapped/bin")
	cfg.CommandEnvFromEnv = map[string]string{"PATH": "AGENTDOCK_TEST_MAPPED_PATH"}

	got, err := svc.CommandEnv("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if value := commandEnvValues(got)["PATH"]; value != "/mapped/bin" {
		t.Fatalf("mapped PATH = %q, want /mapped/bin", value)
	}
}

func TestCommandEnvUsesConfiguredHostMapping(t *testing.T) {
	svc, cfg := newCommandTestService(t)
	t.Setenv("AGENTDOCK_TEST_HOST_FROM_ENV", "host-value")
	unsetEnvForTest(t, "AGENTDOCK_TEST_MISSING_HOST_FROM_ENV")
	cfg.CommandEnvFromEnv = map[string]string{
		"AGENTDOCK_TEST_CHILD_FROM_ENV":   "AGENTDOCK_TEST_HOST_FROM_ENV",
		"AGENTDOCK_TEST_MISSING_FROM_ENV": "AGENTDOCK_TEST_MISSING_HOST_FROM_ENV",
	}

	commandEnv, err := svc.CommandEnv("", nil)
	if err != nil {
		t.Fatal(err)
	}
	values := commandEnvValues(commandEnv)
	if values["AGENTDOCK_TEST_CHILD_FROM_ENV"] != "host-value" {
		t.Fatalf("configured host environment = %#v", values)
	}
	if _, ok := values["AGENTDOCK_TEST_MISSING_FROM_ENV"]; ok {
		t.Fatalf("missing host environment should not create a child variable: %#v", values)
	}

	internalEnv, err := svc.InternalCommandEnv(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := commandEnvValues(internalEnv)["AGENTDOCK_TEST_CHILD_FROM_ENV"]; got != "host-value" {
		t.Fatalf("internal command host environment = %q, want host-value", got)
	}
}

func TestCommandEnvPriorityKeepsSkillAndExplicitOverrides(t *testing.T) {
	svc, cfg := newCommandTestService(t)
	const childKey = "AGENTDOCK_TEST_ENV_PRIORITY"
	t.Setenv("AGENTDOCK_TEST_ENV_PRIORITY_HOST", "host-value")
	cfg.CommandEnvFromEnv = map[string]string{childKey: "AGENTDOCK_TEST_ENV_PRIORITY_HOST"}
	if err := svc.envs.Set(envstore.Scope{Kind: envstore.ScopeSkill, Name: "demo-skill"}, childKey, "skill-value"); err != nil {
		t.Fatal(err)
	}

	skillEnv, err := svc.CommandEnv("demo-skill", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := commandEnvValues(skillEnv)[childKey]; got != "skill-value" {
		t.Fatalf("Skill env priority = %q, want skill-value", got)
	}

	explicitEnv, err := svc.CommandEnv("demo-skill", map[string]string{childKey: "explicit-value"})
	if err != nil {
		t.Fatal(err)
	}
	if got := commandEnvValues(explicitEnv)[childKey]; got != "explicit-value" {
		t.Fatalf("explicit env priority = %q, want explicit-value", got)
	}

	internalEnv, err := svc.InternalCommandEnv(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := commandEnvValues(internalEnv)[childKey]; got != "host-value" {
		t.Fatalf("internal command env = %q, want host-value without Skill env", got)
	}
}

func TestCommandEnvStillBlocksUnconfiguredHostInjectionVariables(t *testing.T) {
	svc, _ := newCommandTestService(t)
	for _, key := range []string{"NODE_OPTIONS", "PYTHONPATH", "JAVA_TOOL_OPTIONS"} {
		t.Setenv(key, "must-not-inherit")
	}

	got, err := svc.CommandEnv("", nil)
	if err != nil {
		t.Fatal(err)
	}
	values := commandEnvValues(got)
	for _, key := range []string{"NODE_OPTIONS", "PYTHONPATH", "JAVA_TOOL_OPTIONS"} {
		if _, ok := values[key]; ok {
			t.Fatalf("unconfigured host variable %s leaked into command environment", key)
		}
	}
}

func commandEnvValues(entries []string) map[string]string {
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	old, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}
