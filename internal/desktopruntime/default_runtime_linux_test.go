//go:build linux

package desktopruntime

import "testing"

func TestDefaultRuntimeRootUsesLinuxInstallDefault(t *testing.T) {
	t.Setenv("AGENTDOCK_RUNTIME_ROOT", "")
	if got := DefaultRuntimeRoot(); got != "/etc/agentdock" {
		t.Fatalf("DefaultRuntimeRoot() = %q, want /etc/agentdock", got)
	}
}
