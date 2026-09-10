//go:build windows

package desktopruntime

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
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

	// 写入“可解析但不是 DPAPI 密文”的内容，模拟跨机器/跨用户迁移后的损坏凭据，
	// 应被视为缺失并重新生成，而不是返回错误。
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
}
