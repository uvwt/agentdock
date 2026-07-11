package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestPrivateNoteSnippetKeepsUTF8Boundaries(t *testing.T) {
	for _, content := range []string{
		strings.Repeat("你", 30) + " token=value " + strings.Repeat("好", 100),
		strings.Repeat("İ", 200) + " TOKEN=value " + strings.Repeat("好", 100),
	} {
		snippet := privateNoteSnippet(content, []string{"token"})
		if !utf8.ValidString(snippet) {
			t.Fatalf("privateNoteSnippet() returned invalid UTF-8: %q", snippet)
		}
		if !strings.Contains(strings.ToLower(snippet), "token=value") {
			t.Fatalf("privateNoteSnippet() lost matched term: %q", snippet)
		}
	}
}

func TestPrivateNotesReadTruncatesAtUTF8Boundary(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	root, err := rt.privateNotesRoot()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, privateNotesPlainDir, "services", "utf8.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("你a"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		maxBytes int
		want     string
	}{
		{maxBytes: 1, want: ""},
		{maxBytes: 3, want: "你"},
		{maxBytes: 4, want: "你a"},
	} {
		result, err := rt.privateNotesRead(context.Background(), map[string]any{
			"path": "notes/services/utf8.md", "max_bytes": test.maxBytes,
		})
		if err != nil {
			t.Fatal(err)
		}
		content := result["content"].(string)
		if content != test.want || !utf8.ValidString(content) {
			t.Fatalf("max_bytes=%d content = %q, want %q", test.maxBytes, content, test.want)
		}
		if got := result["truncated"].(bool); got != (test.maxBytes < 4) {
			t.Fatalf("max_bytes=%d truncated = %v", test.maxBytes, got)
		}
	}
}

func TestPrivateNotesOverwriteRestoresPreviousContentWhenEncryptionFails(t *testing.T) {
	home := t.TempDir()
	cfg := config.Config{AgentDockDefaultDir: t.TempDir(), AgentDockHome: home}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.privateNoteManage(context.Background(), map[string]any{"action": "maintain", "maintenance_action": "init-encryption"}); err != nil {
		t.Fatal(err)
	}
	created, err := rt.privateNoteManage(context.Background(), map[string]any{
		"action": "write", "path": "notes/services/rollback.md", "content": "original-content", "confirmed": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	plainPath := filepath.Join(home, "private-notes", filepath.FromSlash(created["path"].(string)))
	encryptedPath := filepath.Join(home, "private-notes", filepath.FromSlash(created["encrypted_path"].(string)))
	originalPlain, err := os.ReadFile(plainPath)
	if err != nil {
		t.Fatal(err)
	}
	originalEncrypted, err := os.ReadFile(encryptedPath)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("AGENTDOCK_PRIVATE_NOTES_AGE_RECIPIENT", "not-a-valid-age-recipient")
	_, err = rt.privateNoteManage(context.Background(), map[string]any{
		"action": "write", "path": "notes/services/rollback.md", "content": "replacement-content", "confirmed": true, "overwrite": true,
	})
	if err == nil {
		t.Fatal("overwrite succeeded despite invalid encryption recipient")
	}
	plainAfter, readErr := os.ReadFile(plainPath)
	if readErr != nil {
		t.Fatalf("previous plaintext was removed: %v", readErr)
	}
	if string(plainAfter) != string(originalPlain) {
		t.Fatalf("plaintext changed after failed overwrite:\n%s", plainAfter)
	}
	encryptedAfter, readErr := os.ReadFile(encryptedPath)
	if readErr != nil {
		t.Fatalf("previous encrypted backup was removed: %v", readErr)
	}
	if string(encryptedAfter) != string(originalEncrypted) {
		t.Fatal("encrypted backup changed after failed overwrite")
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
