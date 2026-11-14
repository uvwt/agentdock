//go:build linux

package desktopruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLinuxServiceBindingReadsServiceHomeAndManager(t *testing.T) {
	runtimeRoot := t.TempDir()
	serviceHome := filepath.Join(t.TempDir(), "service-home")
	if err := os.WriteFile(filepath.Join(runtimeRoot, "desktop-runtime.json"), []byte(`{
  "schema_version": 1,
  "service_manager": "systemd",
  "service_name": "agentdock-custom",
  "agentdock_binary": "/usr/local/bin/agentdock",
  "environment_file": "`+filepath.Join(runtimeRoot, "agentdock.env")+`"
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeRoot, "agentdock.env"), []byte("AGENTDOCK_HOME='"+serviceHome+"'\nAGENTDOCK_PORT='8765'\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	binding, err := DetectLinuxServiceBinding(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !binding.Configured || !binding.ManagedService() {
		t.Fatalf("unexpected binding: %#v", binding)
	}
	if binding.ServiceManager != "systemd" || binding.ServiceName != "agentdock-custom" {
		t.Fatalf("unexpected service metadata: %#v", binding)
	}
	if binding.AgentDockHome != serviceHome || !binding.MatchesAgentDockHome(serviceHome) {
		t.Fatalf("unexpected AgentDockHome: %#v", binding)
	}
	if binding.MatchesAgentDockHome(filepath.Join(t.TempDir(), "other-home")) {
		t.Fatal("unrelated AgentDockHome should not match service binding")
	}
}

func TestDetectLinuxServiceBindingReturnsUnconfiguredWhenRuntimeRootEmpty(t *testing.T) {
	binding, err := DetectLinuxServiceBinding(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if binding.Configured || binding.ManagedService() {
		t.Fatalf("empty runtime root should not report configured binding: %#v", binding)
	}
}
