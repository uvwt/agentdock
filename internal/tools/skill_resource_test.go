package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestReadFileSupportsSkillURI(t *testing.T) {
	cfg := config.Config{
		AgentDockDefaultDir: t.TempDir(),
		AgentDockHome:       filepath.Join(t.TempDir(), ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	packageDir := installDocumentSkillForTest(t, rt, "demo-skill", "1.0.0", "Read Skill resources in tests.")
	if err := os.MkdirAll(filepath.Join(packageDir, "references"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "references", "guide.md"), []byte("# Guide\n\nUse the safe workflow.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := rt.Call(context.Background(), "read_file", map[string]any{
		"path": "skill://demo-skill/references/guide.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["path"] != "skill://demo-skill/references/guide.md" {
		t.Fatalf("unexpected logical path: %#v", result["path"])
	}
	content, _ := result["content"].(string)
	if !strings.Contains(content, "safe workflow") {
		t.Fatalf("unexpected Skill resource content: %q", content)
	}
}

func TestReadFileRejectsSkillURITraversalAndSymlinkEscape(t *testing.T) {
	cfg := config.Config{
		AgentDockDefaultDir: t.TempDir(),
		AgentDockHome:       filepath.Join(t.TempDir(), ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	packageDir := installDocumentSkillForTest(t, rt, "demo-skill", "1.0.0", "Reject escaping Skill resources.")

	_, err = rt.Call(context.Background(), "read_file", map[string]any{
		"path": "skill://demo-skill/../outside.txt",
	})
	assertToolErrorCode(t, err, "INVALID_SKILL_URI")

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(packageDir, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err = rt.Call(context.Background(), "read_file", map[string]any{
		"path": "skill://demo-skill/escape.txt",
	})
	assertToolErrorCode(t, err, "SKILL_PATH_ESCAPE")
}

func assertToolErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected ToolError %s, got %T: %v", code, err, err)
	}
	if toolErr.Code != code {
		t.Fatalf("ToolError code = %s, want %s: %v", toolErr.Code, code, err)
	}
}
