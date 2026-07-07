package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestWorkspaceEditReplaceAndDeprecatedEditFileAgree(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := rt.Call(context.Background(), "workspace_edit", map[string]any{"action": "replace", "path": "note.txt", "old": "alpha", "new": "beta", "dry_run": true})
	if err != nil {
		t.Fatal(err)
	}
	if result["action"] != "replace" || result["changed"] != true || result["matches"] != 1 {
		t.Fatalf("unexpected workspace_edit result: %#v", result)
	}
	legacy, err := rt.Call(context.Background(), "edit_file", map[string]any{"path": "note.txt", "old": "alpha", "new": "beta", "dry_run": true})
	if err != nil {
		t.Fatal(err)
	}
	if legacy["deprecated"] != true || legacy["replacement_tool"] != "workspace_edit" {
		t.Fatalf("legacy edit_file should advertise workspace_edit replacement: %#v", legacy)
	}
	if legacy["action"] != result["action"] || legacy["matches"] != result["matches"] || legacy["changed"] != result["changed"] {
		t.Fatalf("legacy edit_file core result differs: legacy=%#v new=%#v", legacy, result)
	}
}

func TestGitReadStatusAndDeprecatedGitStatusAgree(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	initGitRepo(t, root)
	result, err := rt.Call(context.Background(), "git_read", map[string]any{"action": "status", "repo_path": "."})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := rt.Call(context.Background(), "git_status", map[string]any{"repo_path": "."})
	if err != nil {
		t.Fatal(err)
	}
	if legacy["deprecated"] != true || legacy["replacement_tool"] != "git_read" {
		t.Fatalf("legacy git_status should advertise git_read replacement: %#v", legacy)
	}
	if result["branch"] != legacy["branch"] || result["clean"] != legacy["clean"] {
		t.Fatalf("git_read status differs from git_status wrapper: new=%#v legacy=%#v", result, legacy)
	}
}

func TestGitReadAvailableInReadOnlyButGitWriteHidden(t *testing.T) {
	root := t.TempDir()
	cfg := testRuntimeConfig(root)
	cfg.ToolProfile = "read-only"
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, name := range rt.ToolNames() {
		seen[name] = true
	}
	if !seen["git_read"] {
		t.Fatalf("read-only profile should expose git_read: %#v", seen)
	}
	if seen["git_write"] {
		t.Fatalf("read-only profile should hide git_write: %#v", seen)
	}
}

func initGitRepo(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{{"init"}, {"config", "user.name", "AgentDock Test"}, {"config", "user.email", "agentdock@example.test"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
}

func testRuntimeConfig(root string) config.Config {
	cfg := config.Config{
		Workspace:       root,
		ToolProfile:     config.ProfileUnified,
		Mode:            config.ModeSandboxed,
		PathPolicy:      config.PathPolicyWorkspace,
		AgentDockDir:    "AgentDock",
		EnableViewImage: true,
	}
	cfg.Normalize()
	return cfg
}
