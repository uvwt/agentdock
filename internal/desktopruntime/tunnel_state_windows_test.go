//go:build windows

package desktopruntime

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNormalizeHTTPSOriginAcceptsMCPURL(t *testing.T) {
	for _, input := range []string{
		"https://yc.188166.top:18443",
		"https://yc.188166.top:18443/",
		"https://yc.188166.top:18443/mcp",
		"https://yc.188166.top:18443/MCP/",
	} {
		origin, err := normalizeHTTPSOrigin(input)
		if err != nil {
			t.Fatalf("normalizeHTTPSOrigin(%q) error = %v", input, err)
		}
		if origin != "https://yc.188166.top:18443" {
			t.Fatalf("normalizeHTTPSOrigin(%q) = %q", input, origin)
		}
	}
}

func TestNormalizeHTTPSOriginRejectsOtherPaths(t *testing.T) {
	if _, err := normalizeHTTPSOrigin("https://yc.188166.top:18443/oauth/token"); err == nil {
		t.Fatal("normalizeHTTPSOrigin() should reject non-MCP paths")
	}
}

func TestReadOrCreateProtectedTextRegeneratesUndecryptable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "auth-token.dpapi")
	const entropy = "agentdock.startup.v1"

	first, err := readOrCreateProtectedText(path, entropy, 32, "Bearer Token")
	if err != nil {
		t.Fatalf("readOrCreateProtectedText() first call error = %v", err)
	}
	if len(first) != 64 {
		t.Fatalf("generated token length = %d, want 64", len(first))
	}

	same, err := readOrCreateProtectedText(path, entropy, 32, "Bearer Token")
	if err != nil {
		t.Fatalf("readOrCreateProtectedText() read-back error = %v", err)
	}
	if same != first {
		t.Fatalf("read-back token changed: got %q, want %q", same, first)
	}

	// 模拟旧安装：没有 SID marker，但凭据文件 ACL 仍明确属于当前用户。
	if err := os.Remove(filepath.Join(root, credentialOwnerSIDFile)); err != nil {
		t.Fatal(err)
	}
	garbage := base64.StdEncoding.EncodeToString([]byte("garbage-not-a-dpapi-blob"))
	if err := os.WriteFile(path, []byte(garbage), 0o600); err != nil {
		t.Fatal(err)
	}
	regenerated, err := readOrCreateProtectedText(path, entropy, 32, "Bearer Token")
	if err != nil {
		t.Fatalf("readOrCreateProtectedText() after corruption error = %v", err)
	}
	if regenerated == first {
		t.Fatal("expected token to be regenerated after corruption")
	}
	if len(regenerated) != 64 {
		t.Fatalf("regenerated token length = %d, want 64", len(regenerated))
	}
	backups, err := filepath.Glob(path + ".unreadable-*.bak")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("unreadable backup count = %d, want 1", len(backups))
	}
	backupData, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(backupData) != garbage {
		t.Fatal("unreadable backup does not preserve the original ciphertext")
	}
	if marker, err := os.ReadFile(filepath.Join(root, credentialOwnerSIDFile)); err != nil || strings.TrimSpace(string(marker)) == "" {
		t.Fatalf("credential owner SID marker missing after recovery: %v", err)
	}
}

func TestReadOrCreateProtectedTextRejectsCredentialOwnerSIDMismatch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "auth-token.dpapi")
	const entropy = "agentdock.startup.v1"

	if _, err := readOrCreateProtectedText(path, entropy, 32, "Bearer Token"); err != nil {
		t.Fatal(err)
	}
	garbage := base64.StdEncoding.EncodeToString([]byte("garbage-not-a-dpapi-blob"))
	if err := os.WriteFile(path, []byte(garbage), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, credentialOwnerSIDFile), []byte("S-1-5-21-1-2-3-424242"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := readOrCreateProtectedText(path, entropy, 32, "Bearer Token"); err == nil || !strings.Contains(err.Error(), "SID 不一致") {
		t.Fatalf("readOrCreateProtectedText() error = %v, want SID mismatch", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != garbage {
		t.Fatal("credential changed despite SID mismatch")
	}
	backups, err := filepath.Glob(path + ".unreadable-*.bak")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("unexpected backup count after rejected recovery = %d", len(backups))
	}
}

func TestReadOrCreateProtectedTextRetriesBeforeRecovery(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "auth-token.dpapi")
	const entropy = "agentdock.startup.v1"
	const existing = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	if err := writeProtectedText(path, existing, entropy); err != nil {
		t.Fatal(err)
	}
	validData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	garbage := base64.StdEncoding.EncodeToString([]byte("temporary-invalid-dpapi"))
	if err := os.WriteFile(path, []byte(garbage), 0o600); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = os.WriteFile(path, validData, 0o600)
	}()

	value, err := readOrCreateProtectedText(path, entropy, 32, "Bearer Token")
	if err != nil {
		t.Fatal(err)
	}
	if value != existing {
		t.Fatalf("readOrCreateProtectedText() = %q, want existing value", value)
	}
	backups, err := filepath.Glob(path + ".unreadable-*.bak")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("transient decrypt failure created %d backup(s), want 0", len(backups))
	}
}

func TestReadOrCreateProtectedTextBoundsUnreadableBackups(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "auth-token.dpapi")
	const entropy = "agentdock.startup.v1"

	if _, err := readOrCreateProtectedText(path, entropy, 32, "Bearer Token"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < generatedCredentialBackupLimit+2; i++ {
		garbage := base64.StdEncoding.EncodeToString([]byte("garbage-not-a-dpapi-blob-" + string(rune('a'+i))))
		if err := os.WriteFile(path, []byte(garbage), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readOrCreateProtectedText(path, entropy, 32, "Bearer Token"); err != nil {
			t.Fatal(err)
		}
	}
	backups, err := filepath.Glob(path + ".unreadable-*.bak")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != generatedCredentialBackupLimit {
		t.Fatalf("unreadable backup count = %d, want %d", len(backups), generatedCredentialBackupLimit)
	}
}

func TestReadOrCreateProtectedTextConcurrentCreateReturnsPersistedValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth-token.dpapi")
	assertConcurrentProtectedTextValue(t, path, "agentdock.startup.v1")
}

func TestReadOrCreateProtectedTextConcurrentRecoveryReturnsPersistedValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth-token.dpapi")
	garbage := base64.StdEncoding.EncodeToString([]byte("garbage-not-a-dpapi-blob"))
	if err := os.WriteFile(path, []byte(garbage), 0o600); err != nil {
		t.Fatal(err)
	}
	assertConcurrentProtectedTextValue(t, path, "agentdock.startup.v1")
}

func assertConcurrentProtectedTextValue(t *testing.T, path, entropy string) {
	t.Helper()

	const workers = 64
	start := make(chan struct{})
	values := make(chan string, workers)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			value, err := readOrCreateProtectedText(path, entropy, 32, "Bearer Token")
			if err != nil {
				errors <- err
				return
			}
			values <- value
		}()
	}

	close(start)
	group.Wait()
	close(values)
	close(errors)
	for err := range errors {
		t.Fatalf("concurrent readOrCreateProtectedText() error = %v", err)
	}

	persisted, err := readProtectedText(path, entropy)
	if err != nil {
		t.Fatalf("read persisted protected text: %v", err)
	}
	count := 0
	for value := range values {
		count++
		if value != persisted {
			t.Fatalf("concurrent value differs from persisted value: got %q, want %q", value, persisted)
		}
	}
	if count != workers {
		t.Fatalf("concurrent result count = %d, want %d", count, workers)
	}
}
