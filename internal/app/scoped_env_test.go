package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestSkillEnvironmentActionsDoNotReturnValuesAndExecCommandUsesPriority(t *testing.T) {
	runtime := newScopedEnvTestRuntime(t)
	defer runtime.Close()
	installDocumentSkillForTest(t, runtime, "demo-skill", "legacy-metadata-only", "Demo Skill.")

	const secret = "skill-secret-value"
	setResult, err := runtime.Call(context.Background(), "skill_manage", map[string]any{
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

	listResult, err := runtime.Call(context.Background(), "skill_manage", map[string]any{
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
		"skill_ref":      "skill://managed/demo-skill",
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
		"skill_ref":      "skill://managed/demo-skill",
		"env":            map[string]any{"DEMO_SECRET": "request-override"},
		"execution_mode": "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if overridden["stdout"] != "request-override" {
		t.Fatalf("explicit env did not override Skill env: %#v", overridden)
	}

	unsetResult, err := runtime.Call(context.Background(), "skill_manage", map[string]any{
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

func TestExecCommandSchemaUsesExactSkillRefOnly(t *testing.T) {
	properties := testInputSchema("exec_command")["properties"].(map[string]any)
	if _, ok := properties["skill_ref"]; !ok {
		t.Fatalf("exec_command schema is missing skill_ref: %#v", properties)
	}
	for _, removed := range []string{"skill", "skill_env"} {
		if _, ok := properties[removed]; ok {
			t.Fatalf("exec_command schema still exposes removed %s compatibility field: %#v", removed, properties)
		}
	}
}

func TestExecCommandSkillRefBindsManagedRootAndEnvironment(t *testing.T) {
	runtime := newScopedEnvTestRuntime(t)
	defer runtime.Close()

	packageDir := installDocumentSkillForTest(t, runtime, "demo-skill", "legacy-metadata-only", "Demo Skill.")
	_, err := runtime.Call(context.Background(), "skill_manage", map[string]any{
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
		"cmd": inspectCommand, "skill_ref": "skill://managed/demo-skill", "execution_mode": "sync",
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
		"skill_ref":      "skill://managed/demo-skill",
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

func TestManagedExecInjectsPrivateSkillDataDirAndRejectsOverrides(t *testing.T) {
	runtime := newScopedEnvTestRuntime(t)
	defer runtime.Close()
	installDocumentSkillForTest(t, runtime, "demo-skill", "metadata-only", "Managed data Skill.")

	expected, err := config.SkillDataDir(runtime.cfg, "demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(expected); !os.IsNotExist(err) {
		t.Fatalf("managed Skill install eagerly created data directory: %v", err)
	}
	command := `printf '%s' "$SKILL_DATA_DIR"`
	if goruntime.GOOS == "windows" {
		command = `[Console]::Write($env:SKILL_DATA_DIR)`
	}
	result, err := runtime.Call(context.Background(), "exec_command", map[string]any{
		"cmd": command, "skill_ref": "skill://managed/demo-skill", "execution_mode": "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sameExistingTestPath(strings.TrimSpace(fmt.Sprint(result["stdout"])), expected) {
		t.Fatalf("SKILL_DATA_DIR = %#v, want path equivalent to %q", result["stdout"], expected)
	}
	info, err := os.Stat(expected)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("SKILL_DATA_DIR is not a directory: %s", expected)
	}
	if goruntime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Fatalf("SKILL_DATA_DIR mode = %o, want 700", info.Mode().Perm())
	}

	if _, err := runtime.Call(context.Background(), "exec_command", map[string]any{
		"cmd": command, "skill_ref": "skill://managed/demo-skill",
		"env": map[string]any{"SKILL_DATA_DIR": t.TempDir()}, "execution_mode": "sync",
	}); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("request env override error = %v, want reserved variable rejection", err)
	}
	if _, err := runtime.Call(context.Background(), "skill_manage", map[string]any{
		"action": "env_set", "skill": "demo-skill", "key": "SKILL_DATA_DIR", "value": t.TempDir(),
	}); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("skill env override error = %v, want reserved variable rejection", err)
	}
}

func TestExecCommandRejectsUnavailableExactSkillRef(t *testing.T) {
	runtime := newScopedEnvTestRuntime(t)
	defer runtime.Close()

	for _, args := range []map[string]any{
		{"cmd": commandNoopForTest(), "skill_ref": "skill://managed/missing-skill"},
		{"cmd": commandNoopForTest(), "skill_ref": "skill://managed/missing-skill", "workdir": t.TempDir()},
	} {
		_, err := runtime.Call(context.Background(), "exec_command", args)
		if err == nil || !strings.Contains(err.Error(), "managed Skill is not installed") {
			t.Fatalf("expected exact missing Skill reference error, got %v", err)
		}
	}
}

func TestSharedSkillWithSameNameDoesNotReceiveManagedEnvironment(t *testing.T) {
	home := t.TempDir()
	setUserHomeForTest(t, home)
	sharedRoot := filepath.Join(home, ".agents", "skills", "demo-skill")
	if err := os.MkdirAll(sharedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(sharedRoot, "SKILL.md"),
		[]byte("---\nname: demo-skill\ndescription: Shared duplicate.\n---\n\n# Shared\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	runtime := newScopedEnvTestRuntime(t)
	defer runtime.Close()
	installDocumentSkillForTest(t, runtime, "demo-skill", "metadata-only", "Managed duplicate.")
	secret := "managed-only-secret"
	if _, err := runtime.Call(context.Background(), "skill_manage", map[string]any{
		"action": "env_set", "skill": "demo-skill", "key": "DEMO_SECRET", "value": secret,
	}); err != nil {
		t.Fatal(err)
	}

	command := `printf '%s' "${DEMO_SECRET-unset}"`
	if goruntime.GOOS == "windows" {
		command = `if ($null -eq $env:DEMO_SECRET) { [Console]::Write("unset") } else { [Console]::Write($env:DEMO_SECRET) }`
	}
	result, err := runtime.Call(context.Background(), "exec_command", map[string]any{
		"cmd": command, "skill_ref": "skill://shared/demo-skill", "execution_mode": "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["stdout"] != "unset" {
		t.Fatalf("shared duplicate received managed Skill environment: %#v", result)
	}

	dataCommand := `printf '%s' "${SKILL_DATA_DIR-unset}"`
	if goruntime.GOOS == "windows" {
		dataCommand = `if ($null -eq $env:SKILL_DATA_DIR) { [Console]::Write("unset") } else { [Console]::Write($env:SKILL_DATA_DIR) }`
	}
	dataResult, err := runtime.Call(context.Background(), "exec_command", map[string]any{
		"cmd": dataCommand, "skill_ref": "skill://shared/demo-skill", "execution_mode": "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dataResult["stdout"] != "unset" {
		t.Fatalf("shared duplicate received managed SKILL_DATA_DIR: %#v", dataResult)
	}
	dataDir, err := config.SkillDataDir(runtime.cfg, "demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("shared Skill invocation created managed data directory: %v", err)
	}
}

func TestWorkspaceSkillDoesNotReceiveManagedDataDir(t *testing.T) {
	runtime := newScopedEnvTestRuntime(t)
	defer runtime.Close()

	workspaceRoot := runtime.cfg.AgentDockDefaultDir
	packageRoot := filepath.Join(workspaceRoot, ".agents", "skills", "workspace-skill")
	if err := os.MkdirAll(packageRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(packageRoot, "SKILL.md"),
		[]byte("---\nname: workspace-skill\ndescription: Workspace only.\n---\n\n# Workspace\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	skillRef, _, err := runtime.skills.WorkspaceSkillRef(workspaceRoot, "workspace-skill")
	if err != nil {
		t.Fatal(err)
	}

	command := `printf '%s' "${SKILL_DATA_DIR-unset}"`
	if goruntime.GOOS == "windows" {
		command = `if ($null -eq $env:SKILL_DATA_DIR) { [Console]::Write("unset") } else { [Console]::Write($env:SKILL_DATA_DIR) }`
	}
	result, err := runtime.Call(context.Background(), "exec_command", map[string]any{
		"cmd": command, "skill_ref": skillRef, "execution_mode": "sync",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["stdout"] != "unset" {
		t.Fatalf("workspace Skill received managed SKILL_DATA_DIR: %#v", result)
	}
	dataDir, err := config.SkillDataDir(runtime.cfg, "workspace-skill")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("workspace Skill invocation created managed data directory: %v", err)
	}
}

func TestExecCommandRejectsRemovedBareSkillFields(t *testing.T) {
	runtime := newScopedEnvTestRuntime(t)
	defer runtime.Close()
	for _, field := range []string{"skill", "skill_env"} {
		_, err := runtime.Call(context.Background(), "exec_command", map[string]any{
			"cmd": commandNoopForTest(), field: "demo-skill",
		})
		if err == nil || !strings.Contains(err.Error(), "declared input schema") {
			t.Fatalf("removed field %s was accepted: %v", field, err)
		}
	}
}

func commandNoopForTest() string {
	if goruntime.GOOS == "windows" {
		return `Write-Output ok`
	}
	return "true"
}
