package app

import (
	"context"
	"fmt"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestSkillEnvironmentActionsDoNotReturnValuesAndExecCommandUsesPriority(t *testing.T) {
	runtime := newScopedEnvTestRuntime(t)
	defer runtime.Close()

	const secret = "skill-secret-value"
	setResult, err := runtime.Call(context.Background(), "skill_package", map[string]any{
		"action": "env_set",
		"skill":  "demo-skill",
		"key":    "DEMO_SECRET",
		"value":  secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(setResult), secret) {
		t.Fatalf("env_set returned secret value: %#v", setResult)
	}

	listResult, err := runtime.Call(context.Background(), "skill_package", map[string]any{
		"action": "env_list",
		"skill":  "demo-skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(listResult), secret) {
		t.Fatalf("env_list returned secret value: %#v", listResult)
	}
	if listResult["count"] != 1 {
		t.Fatalf("unexpected env_list result: %#v", listResult)
	}

	loadedCommand := `test "$DEMO_SECRET" = "skill-secret-value" && printf loaded`
	if goruntime.GOOS == "windows" {
		loadedCommand = `if ($env:DEMO_SECRET -ne "skill-secret-value") { exit 1 }; [Console]::Write("loaded")`
	}
	loaded, err := runtime.Call(context.Background(), "exec_command", map[string]any{
		"cmd":            loadedCommand,
		"skill_env":      "demo-skill",
		"execution_mode": "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded["stdout"] != "loaded" {
		t.Fatalf("Skill environment was not loaded: %#v", loaded)
	}

	overrideCommand := `printf %s "$DEMO_SECRET"`
	if goruntime.GOOS == "windows" {
		overrideCommand = `[Console]::Write($env:DEMO_SECRET)`
	}
	overridden, err := runtime.Call(context.Background(), "exec_command", map[string]any{
		"cmd":            overrideCommand,
		"skill_env":      "demo-skill",
		"env":            map[string]any{"DEMO_SECRET": "request-override"},
		"execution_mode": "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if overridden["stdout"] != "request-override" {
		t.Fatalf("explicit env did not override Skill env: %#v", overridden)
	}

	unsetResult, err := runtime.Call(context.Background(), "skill_package", map[string]any{
		"action": "env_unset",
		"skill":  "demo-skill",
		"key":    "DEMO_SECRET",
	})
	if err != nil {
		t.Fatal(err)
	}
	if unsetResult["removed"] != true {
		t.Fatalf("unexpected env_unset result: %#v", unsetResult)
	}
}

func TestMCPEnvironmentActionsDoNotReturnValues(t *testing.T) {
	runtime := newScopedEnvTestRuntime(t)
	defer runtime.Close()

	_, err := runtime.Call(context.Background(), "mcp_manage", map[string]any{
		"action":      "add",
		"name":        "demo-mcp",
		"description": "Demo MCP for isolated environment tests",
		"transport":   "streamable_http",
		"url":         "http://127.0.0.1:1/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}

	const secret = "mcp-secret-value"
	setResult, err := runtime.Call(context.Background(), "mcp_manage", map[string]any{
		"action": "env_set",
		"name":   " demo-mcp ",
		"key":    "MCP_SECRET",
		"value":  secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(setResult), secret) {
		t.Fatalf("mcp env_set returned secret value: %#v", setResult)
	}

	listResult, err := runtime.Call(context.Background(), "mcp_manage", map[string]any{
		"action": "env_list",
		"name":   " demo-mcp ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(listResult), secret) || listResult["count"] != 1 {
		t.Fatalf("unexpected mcp env_list result: %#v", listResult)
	}

	unsetResult, err := runtime.Call(context.Background(), "mcp_manage", map[string]any{
		"action": "env_unset",
		"name":   " demo-mcp ",
		"key":    "MCP_SECRET",
	})
	if err != nil {
		t.Fatal(err)
	}
	if unsetResult["removed"] != true {
		t.Fatalf("unexpected mcp env_unset result: %#v", unsetResult)
	}
}

func newScopedEnvTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: root,
		AgentDockHome:       filepath.Join(root, ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func TestExecCommandSchemaExposesSkillContext(t *testing.T) {
	properties := InputSchema("exec_command")["properties"].(map[string]any)
	if _, ok := properties["skill"]; !ok {
		t.Fatalf("exec_command schema is missing skill: %#v", properties)
	}
	if _, ok := properties["skill_env"]; !ok {
		t.Fatalf("exec_command schema lost skill_env compatibility: %#v", properties)
	}
}

func TestExecCommandSkillBindsActiveRootAndEnvironment(t *testing.T) {
	runtime := newScopedEnvTestRuntime(t)
	defer runtime.Close()

	packageDir := installDocumentSkillForTest(t, runtime, "demo-skill", "1.0.0", "Demo Skill.")
	_, err := runtime.Call(context.Background(), "skill_package", map[string]any{
		"action": "env_set",
		"skill":  "demo-skill",
		"key":    "DEMO_SECRET",
		"value":  "skill-secret-value",
	})
	if err != nil {
		t.Fatal(err)
	}

	inspectCommand := `printf '%s\n%s' "$PWD" "$DEMO_SECRET"`
	if goruntime.GOOS == "windows" {
		inspectCommand = `[Console]::Write((Get-Location).Path + "` + "`n" + `" + $env:DEMO_SECRET)`
	}
	invocation, err := runtime.Call(context.Background(), "exec_command", map[string]any{
		"cmd": inspectCommand, "skill": "demo-skill", "execution_mode": "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitN(strings.TrimSpace(fmt.Sprint(invocation["stdout"])), "\n", 2)
	if len(lines) != 2 || !sameExistingTestPath(lines[0], packageDir) {
		t.Fatalf("Skill command workdir/output = %#v, want workdir equivalent to %q", invocation["stdout"], packageDir)
	}
	if lines[1] != "skill-secret-value" {
		t.Fatalf("Skill environment value = %q", lines[1])
	}

	overrideDir := t.TempDir()
	overridden, err := runtime.Call(context.Background(), "exec_command", map[string]any{
		"cmd":            inspectCommand,
		"skill":          "demo-skill",
		"workdir":        overrideDir,
		"env":            map[string]any{"DEMO_SECRET": "request-override"},
		"execution_mode": "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	lines = strings.SplitN(strings.TrimSpace(fmt.Sprint(overridden["stdout"])), "\n", 2)
	if len(lines) != 2 || !sameExistingTestPath(lines[0], overrideDir) {
		t.Fatalf("explicit command workdir/output = %#v, want workdir equivalent to %q", overridden["stdout"], overrideDir)
	}
	if lines[1] != "request-override" {
		t.Fatalf("explicit environment override = %q", lines[1])
	}
}

func TestExecCommandSkillRejectsConflictingEnvironmentBinding(t *testing.T) {
	runtime := newScopedEnvTestRuntime(t)
	defer runtime.Close()

	_, err := runtime.Call(context.Background(), "exec_command", map[string]any{
		"cmd":       commandNoopForTest(),
		"skill":     "demo-skill",
		"skill_env": "other-skill",
	})
	if err == nil || !strings.Contains(err.Error(), "must reference the same Skill") {
		t.Fatalf("expected conflicting Skill binding error, got %v", err)
	}
}

func TestExecCommandSkillRequiresActiveVersion(t *testing.T) {
	runtime := newScopedEnvTestRuntime(t)
	defer runtime.Close()

	_, err := runtime.Call(context.Background(), "exec_command", map[string]any{
		"cmd": commandNoopForTest(), "skill": "missing-skill",
	})
	if err == nil || !strings.Contains(err.Error(), "has no active version") {
		t.Fatalf("expected missing active version error, got %v", err)
	}

	_, err = runtime.Call(context.Background(), "exec_command", map[string]any{
		"cmd": commandNoopForTest(), "skill": "missing-skill", "workdir": t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "has no active version") {
		t.Fatalf("expected explicit workdir to keep active Skill validation, got %v", err)
	}
}

func commandNoopForTest() string {
	if goruntime.GOOS == "windows" {
		return `Write-Output ok`
	}
	return "true"
}
