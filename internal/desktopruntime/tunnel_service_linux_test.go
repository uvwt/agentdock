//go:build linux

package desktopruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifiedTunnelServiceActionRejectsImmediateExit(t *testing.T) {
	root := t.TempDir()
	systemctl := filepath.Join(root, "systemctl")
	script := `#!/bin/sh
case "$1" in
  restart|start) exit 0 ;;
  is-active) exit 1 ;;
esac
exit 1
`
	if err := os.WriteFile(systemctl, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	manifest := unixRuntimeManifest{ServiceManager: "systemd", TunnelServiceName: "agentdock-cloudflared"}
	if err := verifiedTunnelServiceAction(context.Background(), manifest, "restart"); err == nil || !strings.Contains(err.Error(), "未保持运行") {
		t.Fatalf("verifiedTunnelServiceAction error = %v, want immediate-exit failure", err)
	}
}

func TestVerifiedTunnelServiceActionAcceptsActiveService(t *testing.T) {
	root := t.TempDir()
	systemctl := filepath.Join(root, "systemctl")
	script := `#!/bin/sh
case "$1" in
  restart|start|is-active) exit 0 ;;
esac
exit 1
`
	if err := os.WriteFile(systemctl, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	manifest := unixRuntimeManifest{ServiceManager: "systemd", TunnelServiceName: "agentdock-cloudflared"}
	if err := verifiedTunnelServiceAction(context.Background(), manifest, "restart"); err != nil {
		t.Fatalf("verifiedTunnelServiceAction error = %v", err)
	}
}
