package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// env 文件的所有权在 Linux 上属于 Go Installer Engine（legacy shell 写入已删除）。
// 契约：managed key 更新、未管理 key 保留、legacy Nexus 凭据清除。
func TestWriteCoreEnvironmentPreservesUnknownAndStripsLegacyNexusKeys(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "agentdock.env")
	initial := strings.Join([]string{
		"AGENTDOCK_HOST=127.0.0.9",
		"AGENTDOCK_PORT=19999",
		"AGENTDOCK_AUTH_TOKEN=stable-token",
		"AGENTDOCK_NEXUS_ENDPOINT=https://legacy.example.test",
		"AGENTDOCK_NEXUS_TOKEN=legacy-secret",
		"AGENTDOCK_BROWSER_ENABLED=true",
		"",
	}, "\n")
	if err := os.WriteFile(envFile, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	request := Request{
		Host:       "127.0.0.1",
		Port:       8765,
		LogLevel:   "info",
		TunnelMode: "none",
		AuthToken:  Specified("stable-token"),
	}
	if err := writeCoreEnvironment(envFile, request); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{
		"AGENTDOCK_NEXUS_ENDPOINT",
		"AGENTDOCK_NEXUS_TOKEN",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("env writer must strip legacy Nexus credential %s:\n%s", forbidden, text)
		}
	}
	for _, want := range []string{
		"AGENTDOCK_BROWSER_ENABLED=true",
		"AGENTDOCK_AUTH_TOKEN=stable-token",
		"AGENTDOCK_HOST=127.0.0.1",
		"AGENTDOCK_PORT=8765",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("env writer lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "AGENTDOCK_SERVER_URL") {
		t.Fatalf("tunnel none must remove the stale server url:\n%s", text)
	}
}

// tunnel token 只落私有 cloudflared.env，绝不走进程参数。
func TestWriteTunnelEnvironmentKeepsTokenOutOfArgv(t *testing.T) {
	tunnelEnv := filepath.Join(t.TempDir(), "cloudflared.env")
	if err := writeTunnelEnvironment(tunnelEnv, "quick", "http://127.0.0.1:8765", "tunnel-secret"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tunnelEnv)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"TUNNEL_TOKEN=tunnel-secret",
		"AGENTDOCK_TUNNEL_MODE=quick",
		"AGENTDOCK_TUNNEL_TARGET=http://127.0.0.1:8765",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("tunnel env missing %q:\n%s", want, text)
		}
	}
}
