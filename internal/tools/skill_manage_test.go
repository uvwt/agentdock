package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/skillruntime"
)

func writeDemoSkillDoc(t *testing.T, pkg string) {
	t.Helper()
	doc := `---
name: demo-skill
description: Use this demo skill in tests.
---

# Demo Skill

This test skill is intentionally small and echoes JSON input.
`
	if err := os.WriteFile(filepath.Join(pkg, "SKILL.md"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSkillManageInstallInspectRunAndList(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "demo-package")
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}

	manifest := `apiVersion: agentdock.dev/v1
kind: Skill
metadata:
  name: demo-skill
  version: 1.0.0
  displayName: Demo Skill
  description: MCP Skill tool test
spec:
  entrypoint: run.sh
  operations:
    - name: echo
      description: Echo JSON input
      inputSchema: {"type":"object","required":["message"],"properties":{"message":{"type":"string"}},"additionalProperties":false}
      outputSchema: {"type":"object","required":["message"],"properties":{"message":{"type":"string"}},"additionalProperties":false}
      timeoutSeconds: 5
  compatibility:
    platforms: [` + runtime.GOOS + `]
    architectures: [` + runtime.GOARCH + `]
    agentdock: ">=1.0.0"
  permissions:
    filesystem: []
    network: []
    secrets: []
    commands: []
`
	if err := os.WriteFile(filepath.Join(pkg, "agentdock.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	writeDemoSkillDoc(t, pkg)
	if err := os.WriteFile(filepath.Join(pkg, "run.sh"), []byte("#!/bin/sh\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Workspace:       root,
		AgentDockDir:    "AgentDock",
		EnableViewImage: true,
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}

	install, err := rt.Call(context.Background(), "skill_manage", map[string]any{
		"action":           "install",
		"source":           "demo-package",
		"activate":         true,
		"confirmed_no_env": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	installResult, ok := install["result"].(skillruntime.InstallResult)
	if !ok || installResult.Skill != "demo-skill" || !installResult.Activated {
		t.Fatalf("unexpected install result: %#v", install["result"])
	}

	inspect, err := rt.Call(context.Background(), "skill_manage", map[string]any{
		"action": "inspect",
		"skill":  "demo-skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspect["version"] != "1.0.0" {
		t.Fatalf("inspect version = %#v", inspect["version"])
	}

	runResult, err := rt.Call(context.Background(), "skill_manage", map[string]any{
		"action":    "run",
		"skill":     "demo-skill",
		"operation": "echo",
		"input":     map[string]any{"message": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, ok := runResult["result"].(skillruntime.RunResult)
	if !ok || !run.OK || string(run.Output) != `{"message":"hello"}` {
		t.Fatalf("unexpected run result: %#v", runResult["result"])
	}

	listed, err := rt.Call(context.Background(), "skill_manage", map[string]any{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if listed["count"] != 1 {
		t.Fatalf("list count = %#v", listed["count"])
	}
}

func TestSkillManageInstallRequiresNoEnvConfirmation(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "demo-package")
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: agentdock.dev/v1
kind: Skill
metadata:
  name: demo-skill
  version: 1.0.0
  displayName: Demo Skill
  description: MCP Skill tool test
spec:
  entrypoint: run.sh
  operations:
    - name: echo
      description: Echo JSON input
      inputSchema: {"type":"object","properties":{},"additionalProperties":false}
      outputSchema: {"type":"object","properties":{},"additionalProperties":false}
      timeoutSeconds: 5
  compatibility:
    platforms: [` + runtime.GOOS + `]
    architectures: [` + runtime.GOARCH + `]
    agentdock: ">=1.0.0"
  permissions:
    filesystem: []
    network: []
    secrets: []
    commands: []
`
	if err := os.WriteFile(filepath.Join(pkg, "agentdock.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	writeDemoSkillDoc(t, pkg)
	if err := os.WriteFile(filepath.Join(pkg, "run.sh"), []byte("#!/bin/sh\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Workspace:    root,
		AgentDockDir: "AgentDock",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}

	_, err = rt.Call(context.Background(), "skill_manage", map[string]any{
		"action": "install",
		"source": "demo-package",
	})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T: %v", err, err)
	}
	if toolErr.Code != skillruntime.ErrManifestInvalid {
		t.Fatalf("error code = %s, want %s: %v", toolErr.Code, skillruntime.ErrManifestInvalid, err)
	}
	if toolErr.Details["stage"] != "manifest.env" {
		t.Fatalf("error stage = %#v, want manifest.env", toolErr.Details["stage"])
	}
}

func TestSkillManageValidateSuccessDoesNotInstall(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "demo-package")
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: agentdock.dev/v1
kind: Skill
metadata:
  name: demo-skill
  version: 1.0.0
  displayName: Demo Skill
  description: MCP Skill validate test
spec:
  entrypoint: run.sh
  operations:
    - name: echo
      description: Echo JSON input
      inputSchema: {"type":"object","properties":{},"additionalProperties":false}
      outputSchema: {"type":"object","properties":{},"additionalProperties":false}
      timeoutSeconds: 5
  compatibility:
    platforms: [` + runtime.GOOS + `]
    architectures: [` + runtime.GOARCH + `]
    agentdock: ">=1.0.0"
  permissions:
    filesystem: []
    network: []
    env:
      - name: DEMO_BASE_URL
        kind: plain
      - name: DEMO_API_TOKEN
        kind: secret
    secrets: []
    commands: [sh]
`
	if err := os.WriteFile(filepath.Join(pkg, "agentdock.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	writeDemoSkillDoc(t, pkg)
	if err := os.WriteFile(filepath.Join(pkg, "run.sh"), []byte("#!/bin/sh\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Workspace:    root,
		AgentDockDir: "AgentDock",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}

	result, err := rt.Call(context.Background(), "skill_manage", map[string]any{
		"action": "validate",
		"source": "demo-package",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["valid"] != true {
		t.Fatalf("validate result = %#v, want valid=true", result)
	}
	if result["digest"] == "" {
		t.Fatalf("validate digest missing: %#v", result)
	}
	manifestResult, ok := result["manifest"].(skillruntime.Manifest)
	if !ok || manifestResult.Metadata.Name != "demo-skill" {
		t.Fatalf("unexpected manifest result: %#v", result["manifest"])
	}
	env, ok := result["env"].([]skillruntime.EnvDefinition)
	if !ok || len(env) != 2 {
		t.Fatalf("unexpected env result: %#v", result["env"])
	}
	commands, ok := result["commands"].([]skillruntime.CommandCheck)
	if !ok || len(commands) != 1 || !commands[0].Found {
		t.Fatalf("unexpected command checks: %#v", result["commands"])
	}
	listed, err := rt.Call(context.Background(), "skill_manage", map[string]any{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	if listed["count"] != 0 {
		t.Fatalf("validate should not install skill; list result: %#v", listed)
	}
}

func TestSkillManageValidateCollectsIssues(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "demo-package")
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: agentdock.dev/v1
kind: Skill
metadata:
  name: demo-skill
  version: 1.0.0
  displayName: Demo Skill
  description: MCP Skill validate test
spec:
  entrypoint: missing.sh
  operations:
    - name: echo
      description: Echo JSON input
      inputSchema: {"type":"object","properties":{},"additionalProperties":false}
      outputSchema: {"type":"object","properties":{},"additionalProperties":false}
      timeoutSeconds: 5
  compatibility:
    platforms: [` + runtime.GOOS + `]
    architectures: [` + runtime.GOARCH + `]
    agentdock: ">=1.0.0"
  permissions:
    filesystem: []
    network: []
    secrets: []
    commands: [agentdock-missing-command-for-test]
`
	if err := os.WriteFile(filepath.Join(pkg, "agentdock.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	writeDemoSkillDoc(t, pkg)
	cfg := config.Config{
		Workspace:    root,
		AgentDockDir: "AgentDock",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}

	result, err := rt.Call(context.Background(), "skill_manage", map[string]any{
		"action": "validate",
		"source": "demo-package",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["valid"] != false {
		t.Fatalf("validate result = %#v, want valid=false", result)
	}
	if manifestResult, ok := result["manifest"].(skillruntime.Manifest); !ok || manifestResult.Metadata.Name != "demo-skill" {
		t.Fatalf("unexpected manifest result: %#v", result["manifest"])
	}
	if result["requires_no_env_confirm"] != true {
		t.Fatalf("requires_no_env_confirm = %#v, want true", result["requires_no_env_confirm"])
	}
	issues, ok := result["issues"].([]skillruntime.ValidateIssue)
	if !ok || len(issues) != 3 {
		t.Fatalf("unexpected issues: %#v", result["issues"])
	}
	stages := map[string]bool{}
	for _, issue := range issues {
		stages[issue.Stage] = true
	}
	for _, stage := range []string{"manifest.entrypoint", "manifest.env", "dependency"} {
		if !stages[stage] {
			t.Fatalf("issues missing stage %q: %#v", stage, issues)
		}
	}
}

func TestSkillManageRejectsUnknownAction(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		Workspace:    root,
		AgentDockDir: "AgentDock",
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Call(context.Background(), "skill_manage", map[string]any{"action": "destroy"}); err == nil {
		t.Fatal("expected invalid action error")
	}
}

func TestEnvManageVerifyRejectsInvalidInputJSON(t *testing.T) {
	rt := newInstalledDemoSkillRuntime(t)

	_, err := rt.Call(context.Background(), "env_manage", map[string]any{
		"action":     "verify",
		"skill":      "demo-skill",
		"operation":  "echo",
		"input_json": "{",
	})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError, got %T: %v", err, err)
	}
	if toolErr.Code != "VALIDATION_ERROR" {
		t.Fatalf("error code = %s, want VALIDATION_ERROR", toolErr.Code)
	}
}

func TestEnvManageVerifyAcceptsStructuredInput(t *testing.T) {
	rt := newInstalledDemoSkillRuntime(t)

	result, err := rt.Call(context.Background(), "env_manage", map[string]any{
		"action":    "verify",
		"skill":     "demo-skill",
		"operation": "echo",
		"input":     map[string]any{"message": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true {
		t.Fatalf("verify failed: %#v", result)
	}
	run, ok := result["result"].(skillruntime.RunResult)
	if !ok || !run.OK || string(run.Output) != `{"message":"hello"}` {
		t.Fatalf("unexpected verify result: %#v", result["result"])
	}
}

func TestToolsCompatEnvDefinitionsUseSharedSet(t *testing.T) {
	byKey := map[string]struct{}{}
	for _, def := range compatEnvDefinitions() {
		byKey[def.Skill+"\x00"+def.Name] = struct{}{}
	}
	for _, key := range []string{
		"baidu-netdisk\x00BDPAN_CONFIG_FILE",
		"bark\x00BARK_SERVER_URL",
		"cloudsaver\x00CLOUDSAVER_PASSWORD",
		"telegram-official\x00TELEGRAM_CHAT_ID",
		"xiaohongshu-mcp\x00XIAOHONGSHU_MCP_URL",
	} {
		if _, ok := byKey[key]; !ok {
			t.Fatalf("missing compat env definition %s", key)
		}
	}
}

func newInstalledDemoSkillRuntime(t *testing.T) *Runtime {
	t.Helper()
	root := t.TempDir()
	pkg := filepath.Join(root, "demo-package")
	if err := os.MkdirAll(pkg, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: agentdock.dev/v1
kind: Skill
metadata:
  name: demo-skill
  version: 1.0.0
  displayName: Demo Skill
  description: MCP Skill tool test
spec:
  entrypoint: run.sh
  operations:
    - name: echo
      description: Echo JSON input
      inputSchema: {"type":"object","required":["message"],"properties":{"message":{"type":"string"}},"additionalProperties":false}
      outputSchema: {"type":"object","required":["message"],"properties":{"message":{"type":"string"}},"additionalProperties":false}
      timeoutSeconds: 5
  compatibility:
    platforms: [` + runtime.GOOS + `]
    architectures: [` + runtime.GOARCH + `]
    agentdock: ">=1.0.0"
  permissions:
    filesystem: []
    network: []
    secrets: []
    commands: []
`
	if err := os.WriteFile(filepath.Join(pkg, "agentdock.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	writeDemoSkillDoc(t, pkg)
	if err := os.WriteFile(filepath.Join(pkg, "run.sh"), []byte("#!/bin/sh\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Workspace:       root,
		AgentDockDir:    "AgentDock",
		EnableViewImage: true,
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Call(context.Background(), "skill_manage", map[string]any{
		"action":           "install",
		"source":           "demo-package",
		"activate":         true,
		"confirmed_no_env": true,
	}); err != nil {
		t.Fatal(err)
	}
	return rt
}
