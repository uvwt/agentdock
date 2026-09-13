package installer

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/updateengine"
)

func TestInspectRepairsProjectionFromTerminalTransaction(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	txID := "0123456789abcdef0123456789abcdef"
	transaction := Transaction{
		SchemaVersion: SchemaVersion,
		TransactionID: txID,
		Platform:      "windows",
		Action:        ActionInstall,
		TargetVersion: "v0.8.3",
		ActiveVersion: "v0.8.3",
		State:         updateengine.StateTrial,
		Phase:         PhaseCommit,
		InstallRoot:   filepath.Join(root, "bin"),
		RuntimeRoot:   root,
		StartedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.WriteTransaction(transaction); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteResult(Result{
		SchemaVersion: SchemaVersion,
		TransactionID: txID,
		Platform:      "windows",
		Action:        ActionInstall,
		State:         updateengine.StateTrial,
		Phase:         PhaseCommit,
		Version:       "v0.8.3",
		ActiveVersion: "v0.8.3",
		LocalMCPURL:   "http://127.0.0.1:8765/mcp",
		PublicURL:     "https://example.test",
		Healthy:       true,
		StartedAt:     now,
	}); err != nil {
		t.Fatal(err)
	}

	// 模拟 Complete 已把权威 transaction 写成 committed，但进程在覆盖 result projection 前崩溃。
	completedAt := now.Add(time.Second)
	transaction.State = updateengine.StateCommitted
	transaction.Phase = PhaseCommit
	transaction.CompletedAt = &completedAt
	if err := store.WriteTransaction(transaction); err != nil {
		t.Fatal(err)
	}

	inspection, err := Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := AssertCommitted(inspection, "v0.8.3"); err != nil {
		t.Fatal(err)
	}
	if inspection.LocalMCPURL != "http://127.0.0.1:8765/mcp" || inspection.PublicURL != "https://example.test" || !inspection.Healthy {
		t.Fatalf("committed projection fields were not preserved: %#v", inspection)
	}

	current, err := store.ReadCurrentResult()
	if err != nil {
		t.Fatal(err)
	}
	if current.State != updateengine.StateCommitted || current.TransactionID != txID {
		t.Fatalf("current projection was not repaired from terminal transaction: %#v", current)
	}
	if !current.CompletedAt.Equal(completedAt) {
		t.Fatalf("completed_at=%s, want %s", current.CompletedAt, completedAt)
	}
}
