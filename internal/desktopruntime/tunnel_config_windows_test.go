//go:build windows

package desktopruntime

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformConfigureTunnelRollsBackFailedNamedStart(t *testing.T) {
	root := t.TempDir()
	newURL := "https://new.example.test"
	oldToken := "old-stable-token"
	newToken := "new-replacement-token"

	healthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthServer.Close()
	addr := healthServer.Listener.Addr().(*net.TCPAddr)

	runValueName := "AgentDockTunnelRollbackTest-" + filepath.Base(root)
	defer func() { _ = removeRunValue(runValueName) }()

	manifest := Manifest{
		SchemaVersion:               SchemaVersion,
		InstallRoot:                 root,
		AgentDockBinary:             filepath.Join(root, "bin", "agentdock.exe"),
		CloudflaredBinary:           filepath.Join(root, "bin", "missing-cloudflared.exe"),
		CloudflaredStartupValueName: runValueName,
		Host:                        addr.IP.String(),
		Port:                        addr.Port,
		LocalMCPURL:                 "http://127.0.0.1:8765/mcp",
		TunnelMode:                  "none",
		InstallChannel:              "test",
		PrivilegeMode:               "standard",
	}
	manifestPath := filepath.Join(root, "runtime.json")
	if err := Save(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	modePath := filepath.Join(root, "cloudflared-mode.txt")
	if err := writeRuntimeText(modePath, "none"); err != nil {
		t.Fatal(err)
	}
	serverURLPath := filepath.Join(root, "server-url.txt")
	if err := writeRuntimeText(serverURLPath, ""); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(root, "cloudflared-token.dpapi")
	if err := writeProtectedText(tokenPath, oldToken, tunnelTokenEntropy); err != nil {
		t.Fatal(err)
	}

	tokenFile := filepath.Join(root, "replacement-token.txt")
	if err := os.WriteFile(tokenFile, []byte(newToken), 0o600); err != nil {
		t.Fatal(err)
	}

	err := platformConfigureTunnel(context.Background(), TunnelConfigureRequest{
		RuntimeRoot: root,
		Mode:        "named",
		ServerURL:   newURL,
		TokenFile:   tokenFile,
	})
	if err == nil || !strings.Contains(err.Error(), "找不到 cloudflared.exe") {
		t.Fatalf("expected cloudflared start failure, got %v", err)
	}

	storedToken, err := readProtectedText(tokenPath, tunnelTokenEntropy)
	if err != nil {
		t.Fatal(err)
	}
	if storedToken != oldToken {
		t.Fatalf("stored token = %q, want previous token", storedToken)
	}
	namedURLPath := filepath.Join(root, "named-server-url.txt")
	if _, err := os.Stat(namedURLPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("named URL was not rolled back: %v", err)
	}
	serverURL, err := readTrimmedText(serverURLPath)
	if err != nil {
		t.Fatal(err)
	}
	if serverURL != "" {
		t.Fatalf("server URL = %q, want empty", serverURL)
	}
	mode, err := readTrimmedText(modePath)
	if err != nil {
		t.Fatal(err)
	}
	if mode != "none" {
		t.Fatalf("mode = %q, want none", mode)
	}
	restoredManifest, err := Load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if restoredManifest.TunnelMode != "none" || restoredManifest.PublicURL != "" {
		t.Fatalf("manifest was not rolled back: mode=%q public_url=%q", restoredManifest.TunnelMode, restoredManifest.PublicURL)
	}
	startupEnabled, err := tunnelAutostartEnabled(restoredManifest)
	if err != nil {
		t.Fatal(err)
	}
	if startupEnabled {
		t.Fatal("tunnel autostart was not rolled back")
	}
}
