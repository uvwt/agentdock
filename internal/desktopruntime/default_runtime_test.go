package desktopruntime

import "testing"

func TestDefaultRuntimeRootUsesEnvironmentOverride(t *testing.T) {
	want := t.TempDir()
	t.Setenv("AGENTDOCK_RUNTIME_ROOT", "  "+want+"  ")
	if got := DefaultRuntimeRoot(); got != want {
		t.Fatalf("DefaultRuntimeRoot() = %q, want %q", got, want)
	}
}
