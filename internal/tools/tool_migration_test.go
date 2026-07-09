package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestFileEditReplace(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := rt.Call(context.Background(), "file_edit", map[string]any{"action": "replace", "path": "note.txt", "old": "alpha", "new": "beta", "dry_run": true})
	if err != nil {
		t.Fatal(err)
	}
	if result["action"] != "replace" || result["changed"] != true || result["matches"] != 1 {
		t.Fatalf("unexpected file_edit result: %#v", result)
	}
}

func TestFileEditAddMoveDelete(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	result, err := rt.Call(context.Background(), "file_edit", map[string]any{"action": "add", "path": "draft.txt", "content": "hello\n"})
	if err != nil {
		t.Fatal(err)
	}
	if result["changed"] != true {
		t.Fatalf("expected add to change file: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "draft.txt")); err != nil {
		t.Fatalf("expected added file: %v", err)
	}
	result, err = rt.Call(context.Background(), "file_edit", map[string]any{"action": "move", "path": "draft.txt", "new_path": "final.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if result["new_path"] != "final.txt" {
		t.Fatalf("unexpected move result: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "final.txt")); err != nil {
		t.Fatalf("expected moved file: %v", err)
	}
	result, err = rt.Call(context.Background(), "file_edit", map[string]any{"action": "delete", "path": "final.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if result["changed"] != true {
		t.Fatalf("expected delete to report changed: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "final.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted, err=%v", err)
	}
}

func TestGitReadStatus(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	initGitRepo(t, root)
	result, err := rt.Call(context.Background(), "git_read", map[string]any{"action": "status", "repo_path": "."})
	if err != nil {
		t.Fatal(err)
	}
	if result["action"] != "status" || result["clean"] != true {
		t.Fatalf("unexpected git_read status result: %#v", result)
	}
}

func TestGitReadGitHubRepoAccessMissingCredential(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	result, err := rt.Call(context.Background(), "git_read", map[string]any{"action": "github_repo_access", "repo": "owner/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if result["action"] != "github_repo_access" || result["credential_found"] != false {
		t.Fatalf("unexpected git_read github_repo_access result: %#v", result)
	}
}

func TestLegacyModelEntrypointsRemovedFromRuntime(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	for _, name := range []string{"apply_patch", "edit_file", "workspace_repos", "git_status", "git_diff", "git_log", "git_inspect", "git_remote", "git_clone", "git_commit", "check_github_repo_access", "browser_profile"} {
		if _, err := rt.Call(context.Background(), name, map[string]any{}); err == nil {
			t.Fatalf("legacy tool should not be callable: %s", name)
		}
	}
}

func TestWorkflowTemplateMatchIsOnlyTemplateDiscoveryEntrypoint(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	result, err := rt.Call(context.Background(), "workflow_template_manage", map[string]any{"action": "match", "goal": "deploy AgentDock", "device": "DockMini"})
	if err != nil {
		t.Fatal(err)
	}
	if result["action"] != "match" {
		t.Fatalf("unexpected workflow template match result: %#v", result)
	}
	if _, err := rt.Call(context.Background(), "task_manage", map[string]any{"action": "template_match", "goal": "deploy AgentDock"}); err == nil {
		t.Fatal("task_manage template_match should not be callable")
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
		AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock"),
		EnableViewImage: true,
	}
	if err := cfg.Normalize(); err != nil {
		panic(err)
	}
	return cfg
}
