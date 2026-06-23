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
	root := filepath.Join(t.TempDir(), "private-notes")
	rt, err := NewRuntime(config.Config{Workspace: t.TempDir(), AgentDockDir: "AgentDock", PrivateNotesDir: root, ToolProfile: config.ProfileUnified, EnableViewImage: true})
	if err != nil {
		t.Fatal(err)
	}
	marker := "abc123"
	content := "# Embedding\n\n" + "EMBEDDING_" + "TOKEN=" + marker + "\n"
	write, err := rt.privateNotesWrite(context.Background(), map[string]any{"title": "Embedding 200399", "category": "services", "content": content, "confirmed": true})
	if err != nil {
		t.Fatal(err)
	}
	p := write["path"].(string)
	enc := write["encrypted_path"].(string)
	if !strings.HasPrefix(p, "notes/services/") || !strings.HasSuffix(enc, ".enc") {
		t.Fatalf("unexpected paths: %q %q", p, enc)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(enc))); err != nil {
		t.Fatalf("encrypted backup missing: %v", err)
	}
	search, err := rt.privateNotesSearch(context.Background(), map[string]any{"query": "embedding"})
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
	read, err := rt.privateNotesRead(context.Background(), map[string]any{"path": p})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read["content"].(string), marker) {
		t.Fatalf("read should return plaintext")
	}
	check, err := rt.privateNotesMaintain(context.Background(), map[string]any{"action": "check"})
	if err != nil {
		t.Fatal(err)
	}
	if !check["ok"].(bool) {
		t.Fatalf("check failed: %#v", check)
	}
}
