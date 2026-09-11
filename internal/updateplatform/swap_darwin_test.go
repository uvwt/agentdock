//go:build darwin

package updateplatform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSwapPathsAtomicExchangesSiblingDirectories(t *testing.T) {
	root := t.TempDir()
	active := filepath.Join(root, "AgentDock.app")
	trial := filepath.Join(root, ".AgentDock.app.trial")
	if err := os.Mkdir(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(trial, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(active, "version"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trial, "version"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := swapPathsAtomic(active, trial); err != nil {
		t.Fatalf("swapPathsAtomic() error = %v", err)
	}
	activeVersion, err := os.ReadFile(filepath.Join(active, "version"))
	if err != nil {
		t.Fatal(err)
	}
	trialVersion, err := os.ReadFile(filepath.Join(trial, "version"))
	if err != nil {
		t.Fatal(err)
	}
	if string(activeVersion) != "new\n" || string(trialVersion) != "old\n" {
		t.Fatalf("swap content = active %q, trial %q", activeVersion, trialVersion)
	}

	if err := swapPathsAtomic(active, trial); err != nil {
		t.Fatalf("rollback swap error = %v", err)
	}
	activeVersion, _ = os.ReadFile(filepath.Join(active, "version"))
	trialVersion, _ = os.ReadFile(filepath.Join(trial, "version"))
	if string(activeVersion) != "old\n" || string(trialVersion) != "new\n" {
		t.Fatalf("rollback content = active %q, trial %q", activeVersion, trialVersion)
	}
}
