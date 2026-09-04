package desktopruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindQuickTunnelURL(t *testing.T) {
	tests := []struct {
		name string
		log  string
		want string
	}{
		{
			name: "cloudflared success output",
			log: "2026-08-25T02:35:41Z INF Requesting new quick Tunnel on trycloudflare.com...\n" +
				"2026-08-25T02:35:42Z INF Your quick Tunnel has been created! Visit it at:\n" +
				"https://serves-chemical-remains-shows.trycloudflare.com\n" +
				"2026-08-25T02:35:43Z INF Registered tunnel connection\n",
			want: "https://serves-chemical-remains-shows.trycloudflare.com",
		},
		{
			name: "success marker and url on same line",
			log:  "INF Your quick Tunnel has been created! Visit it at: https://same-line.trycloudflare.com\n",
			want: "https://same-line.trycloudflare.com",
		},
		{
			name: "issue 28 provisioning failure",
			log: "2026-08-25T02:35:41Z INF Requesting new quick Tunnel on trycloudflare.com...\n" +
				"failed to request quick Tunnel: Post \"https://api.trycloudflare.com/tunnel\": read tcp 192.168.1.8:64268->104.16.231.132:443: wsarecv: An existing connection was forcibly closed by the remote host.\n",
		},
		{
			name: "unrelated trycloudflare url before success marker",
			log: "diagnostic endpoint https://api.trycloudflare.com\n" +
				"INF Your quick Tunnel has been created! Visit it at:\n" +
				"https://actual-tunnel.trycloudflare.com\n",
			want: "https://actual-tunnel.trycloudflare.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findQuickTunnelURL([]byte(tt.log)); got != tt.want {
				t.Fatalf("findQuickTunnelURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadQuickTunnelLogSinceSkipsPreviousGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloudflared.err.log")
	oldLog := "INF Your quick Tunnel has been created! Visit it at:\nhttps://old.trycloudflare.com\n"
	if err := os.WriteFile(path, []byte(oldLog), 0o600); err != nil {
		t.Fatal(err)
	}
	cursor, err := captureQuickTunnelLogCursor(path)
	if err != nil {
		t.Fatal(err)
	}

	newLog := "INF Your quick Tunnel has been created! Visit it at:\nhttps://new.trycloudflare.com\n"
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(newLog); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := readQuickTunnelLogSince(path, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if got := findQuickTunnelURL(data); got != "https://new.trycloudflare.com" {
		t.Fatalf("findQuickTunnelURL(new generation) = %q", got)
	}
}

func TestReadQuickTunnelLogSinceReadsResetLogFromStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cloudflared.err.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 1024)), 0o600); err != nil {
		t.Fatal(err)
	}
	cursor, err := captureQuickTunnelLogCursor(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	newLog := "INF Your quick Tunnel has been created! Visit it at:\nhttps://rotated.trycloudflare.com\n"
	if err := os.WriteFile(path, []byte(newLog), 0o600); err != nil {
		t.Fatal(err)
	}

	data, err := readQuickTunnelLogSince(path, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if got := findQuickTunnelURL(data); got != "https://rotated.trycloudflare.com" {
		t.Fatalf("findQuickTunnelURL(rotated generation) = %q", got)
	}
}
