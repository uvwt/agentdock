package session

import (
	"testing"
	"time"
)

func TestExecutionContextIsIncludedInSnapshotAndSummary(t *testing.T) {
	s := &Session{
		ID:        "session-test",
		StartedAt: time.Now(),
		Done:      make(chan struct{}),
		Terminal:  "conpty",
	}
	s.SetExecutionContext(ExecutionContext{
		Runtime:      "wsl",
		Distribution: "Ubuntu",
		Workdir:      "/mnt/d/Project/synapse",
	})

	snapshot := s.Snapshot("running", 1024)
	if snapshot["runtime"] != "wsl" || snapshot["wsl_distribution"] != "Ubuntu" || snapshot["workdir"] != "/mnt/d/Project/synapse" {
		t.Fatalf("snapshot execution metadata = %#v", snapshot)
	}

	summary := s.Summary()
	if summary.Runtime != "wsl" || summary.Distribution != "Ubuntu" || summary.Workdir != "/mnt/d/Project/synapse" {
		t.Fatalf("summary execution metadata = %#v", summary)
	}
}
