//go:build darwin || linux

package desktopruntime

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestDarwinExecutableFromAppBundle(t *testing.T) {
	if !darwinExecutableFromAppBundle("/Applications/AgentDock.app/Contents/Helpers/agentdock") {
		t.Fatal("App Helper must be treated as SMAppService install")
	}
	if !darwinExecutableFromAppBundle(`/Applications/AgentDock.app/Contents/MacOS/AgentDock`) {
		t.Fatal("App executable must be treated as SMAppService install")
	}
	if darwinExecutableFromAppBundle(filepath.Join("/Users/me", ".local", "bin", "agentdock")) {
		t.Fatal("CLI binary must not be treated as App Bundle")
	}
}

func TestLoadUnixRuntimeCLIUsesLaunchAgentLabels(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("CLI vs App Bundle dispatch is Darwin-only")
	}
	root := t.TempDir()
	manifest, _, err := loadUnixRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ServiceManager != "launchd" {
		t.Fatalf("test binary is not an App Bundle, manager=%s", manifest.ServiceManager)
	}
	if manifest.ServiceName != "com.uvwt.agentdock" || manifest.TunnelServiceName != "com.uvwt.agentdock.cloudflared" {
		t.Fatalf("CLI labels=%s %s", manifest.ServiceName, manifest.TunnelServiceName)
	}
}
