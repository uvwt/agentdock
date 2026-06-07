package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/skillruntime"
)

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
	if err := os.WriteFile(filepath.Join(pkg, "run.sh"), []byte("#!/bin/sh\ncat\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Workspace:       root,
		ToolProfile:     config.ProfileUnified,
		Mode:            config.ModeSandboxed,
		PathPolicy:      config.PathPolicyWorkspace,
		AgentDockDir:    "AgentDock",
		PluginDir:       "plugins",
		EnableViewImage: true,
	}
	cfg.Normalize()
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}

	install, err := rt.Call(context.Background(), "skill_manage", map[string]any{
		"action":   "install",
		"source":   "demo-package",
		"activate": true,
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

func TestSkillManageRejectsUnknownAction(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{
		Workspace:    root,
		ToolProfile:  config.ProfileUnified,
		Mode:         config.ModeSandboxed,
		PathPolicy:   config.PathPolicyWorkspace,
		AgentDockDir: "AgentDock",
	}
	cfg.Normalize()
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Call(context.Background(), "skill_manage", map[string]any{"action": "destroy"}); err == nil {
		t.Fatal("expected invalid action error")
	}
}
