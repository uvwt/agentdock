package tools

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
		"cmd":             loadedCommand,
		"skill_env":       "demo-skill",
		"wait_until_exit": true,
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
		"cmd":             overrideCommand,
		"skill_env":       "demo-skill",
		"env":             map[string]any{"DEMO_SECRET": "request-override"},
		"wait_until_exit": true,
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
		"name":   "demo-mcp",
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
		"name":   "demo-mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(listResult), secret) || listResult["count"] != 1 {
		t.Fatalf("unexpected mcp env_list result: %#v", listResult)
	}

	unsetResult, err := runtime.Call(context.Background(), "mcp_manage", map[string]any{
		"action": "env_unset",
		"name":   "demo-mcp",
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
