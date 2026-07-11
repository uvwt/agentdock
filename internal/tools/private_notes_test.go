package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestPrivateNotesWriteReadSearchAndEncrypt(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "private-notes")
	cfg := config.Config{AgentDockDefaultDir: t.TempDir(), AgentDockHome: home}
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

func TestPrivateNotesSearchHonorsCanceledContext(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	root, err := rt.privateNotesRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, privateNotesPlainDir), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = rt.privateNotesSearch(ctx, map[string]any{"query": "anything"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("privateNotesSearch() error = %v, want context.Canceled", err)
	}
}

func TestPrivateNotesSearchReportsUnreadableEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}
	rt, _ := newCodeToolsRuntime(t)
	root, err := rt.privateNotesRoot()
	if err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(root, privateNotesPlainDir)
	if err := os.MkdirAll(notes, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-target", filepath.Join(notes, "broken.md")); err != nil {
		t.Fatal(err)
	}
	_, err = rt.privateNotesSearch(context.Background(), map[string]any{"query": "anything"})
	if err == nil || !strings.Contains(err.Error(), "read private note") {
		t.Fatalf("privateNotesSearch() error = %v, want read failure", err)
	}
	if _, err := listPrivateNotes(root); err == nil || !strings.Contains(err.Error(), "read private note") {
		t.Fatalf("listPrivateNotes() error = %v, want read failure", err)
	}
}

func TestPrivateNotesRecipientsPreserveReadFailure(t *testing.T) {
	t.Setenv("AGENTDOCK_PRIVATE_NOTES_AGE_RECIPIENT", "")
	t.Setenv("AGENTDOCK_PRIVATE_NOTES_AGE_RECIPIENTS_FILE", "")
	root := t.TempDir()
	recipientPath := filepath.Join(root, privateNotesKeyDir, privateNotesRecipientFile)
	if err := os.MkdirAll(recipientPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := privateNotesAgeRecipients(root); err == nil || !strings.Contains(err.Error(), "read private notes recipients") {
		t.Fatalf("privateNotesAgeRecipients() error = %v, want read failure", err)
	}
}
