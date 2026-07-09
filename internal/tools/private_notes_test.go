package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestPrivateNotesWriteReadSearchAndEncrypt(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "private-notes")
	cfg := config.Config{AgentDockDefaultDir: t.TempDir(), AgentDockHome: home, EnableViewImage: true}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	initResult, err := rt.privateNoteManage(context.Background(), map[string]any{"action": "maintain", "maintenance_action": "init-encryption"})
	if err != nil {
		t.Fatal(err)
	}
	if initResult["recipient"] == "" {
		t.Fatalf("missing age recipient: %#v", initResult)
	}
	marker := "abc123"
	content := "# Embedding\n\n" + "EMBEDDING_" + "TOKEN=" + marker + "\n"
	write, err := rt.privateNoteManage(context.Background(), map[string]any{"action": "write", "title": "Embedding 200399", "category": "services", "content": content, "confirmed": true})
	if err != nil {
		t.Fatal(err)
	}
	p := write["path"].(string)
	enc := write["encrypted_path"].(string)
	if !strings.HasPrefix(p, "notes/services/") || !strings.HasSuffix(enc, ".md.age") {
		t.Fatalf("unexpected paths: %q %q", p, enc)
	}
	encPath := filepath.Join(root, filepath.FromSlash(enc))
	if _, err := os.Stat(encPath); err != nil {
		t.Fatalf("encrypted backup missing: %v", err)
	}
	encBytes, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encBytes), marker) {
		t.Fatalf("encrypted backup should not contain plaintext marker")
	}
	search, err := rt.privateNoteManage(context.Background(), map[string]any{"action": "search", "query": "embedding"})
	if err != nil {
		t.Fatal(err)
	}
	results := search["results"].([]map[string]any)
	if len(results) != 1 {
		t.Fatalf("expected one result, got %#v", results)
	}
	if strings.Contains(results[0]["snippet"].(string), marker) {
		t.Fatalf("search snippet leaked marker: %#v", results[0])
	}
	read, err := rt.privateNoteManage(context.Background(), map[string]any{"action": "read", "path": p})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read["content"].(string), marker) {
		t.Fatalf("read should return plaintext")
	}
	check, err := rt.privateNoteManage(context.Background(), map[string]any{"action": "status", "status_action": "check"})
	if err != nil {
		t.Fatal(err)
	}
	if !check["ok"].(bool) {
		t.Fatalf("check failed: %#v", check)
	}
}
