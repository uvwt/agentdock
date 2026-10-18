//go:build windows

package desktopruntime

import (
	"path/filepath"
	"testing"
)

func TestDefaultRuntimeRootUsesWindowsInstallDefault(t *testing.T) {
	t.Setenv("AGENTDOCK_RUNTIME_ROOT", "")
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)
	want := filepath.Join(localAppData, "AgentDock")
	if got := DefaultRuntimeRoot(); got != want {
		t.Fatalf("DefaultRuntimeRoot() = %q, want %q", got, want)
	}
}
