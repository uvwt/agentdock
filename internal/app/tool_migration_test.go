package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
	assertToolResultMatchesOutputSchema(t, "file_edit", result)
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
	assertToolResultMatchesOutputSchema(t, "file_edit", result)
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
	assertToolResultMatchesOutputSchema(t, "file_edit", result)
	if result["changed"] != true {
		t.Fatalf("expected delete to report changed: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "final.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted, err=%v", err)
	}
}

func TestWorkflowVectorIndexUsesCanonicalMCPFields(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	result, err := rt.Call(context.Background(), "workflow_template_manage", map[string]any{"action": "vector_index"})
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchesOutputSchema(t, "workflow_template_manage", result)
	if result["action"] != "vector_index" || result["vector_index_available"] != false {
		t.Fatalf("vector_index result = %#v", result)
	}
	if _, legacy := result["available"]; legacy {
		t.Fatalf("workflow MCP result leaked REST-only available field: %#v", result)
	}
}

func TestLegacyModelEntrypointsRemovedFromRuntime(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	for _, name := range []string{"apply_patch", "edit_file", "workspace_repos", "git_read", "git_write", "git_status", "git_diff", "git_log", "git_inspect", "git_remote", "git_clone", "git_commit", "check_github_repo_access", "browser_profile"} {
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
	assertToolResultMatchesOutputSchema(t, "workflow_template_manage", result)
	if result["action"] != "match" {
		t.Fatalf("unexpected workflow template match result: %#v", result)
	}
	if _, err := rt.Call(context.Background(), "task_manage", map[string]any{"action": "template_match", "goal": "deploy AgentDock"}); err == nil {
		t.Fatal("task_manage template_match should not be callable")
	}
}
