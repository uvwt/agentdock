package installer

import (
	"os"
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

// inspect 必须给脚本结构化的 pointer / self-update 事务结论，
// Setup 和卸载脚本不得再手工解析 active-version.json 或 update/transaction.json。
func TestInspectReportsPointerAndPendingUpdateTransaction(t *testing.T) {
	root := t.TempDir()

	empty, err := Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if empty.PointerState != "missing" || empty.PendingUpdateTransaction {
		t.Fatalf("empty root: pointer_state=%q pending_update=%t", empty.PointerState, empty.PendingUpdateTransaction)
	}

	updateStore, err := updateengine.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := updateStore.WriteActive(updateengine.ActiveVersion{
		SchemaVersion:   updateengine.SchemaVersion,
		ActiveVersion:   "v1.0.0",
		FallbackVersion: "v0.9.0",
		State:           updateengine.StateCommitted,
	}); err != nil {
		t.Fatal(err)
	}
	committed, err := Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if committed.PointerState != string(updateengine.StateCommitted) ||
		committed.PointerActiveVersion != "v1.0.0" ||
		committed.PointerFallbackVersion != "v0.9.0" {
		t.Fatalf("committed pointer not surfaced: %#v", committed)
	}

	// update/transaction.json 只按存在性判断：脚本只需要知道“要先跑 self-update 恢复”。
	updateDir := filepath.Join(root, "update")
	if err := os.MkdirAll(updateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updateDir, "transaction.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	pending, err := Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !pending.PendingUpdateTransaction {
		t.Fatalf("pending update transaction was not reported: %#v", pending)
	}

	// trial pointer 必须原样透传 state，脚本据此把布局视为未收敛。
	if err := updateStore.WriteActive(updateengine.ActiveVersion{
		SchemaVersion: updateengine.SchemaVersion,
		ActiveVersion: "v1.1.0",
		State:         updateengine.StateTrial,
		TransactionID: "0123456789abcdef0123456789abcdef",
	}); err != nil {
		t.Fatal(err)
	}
	trial, err := Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if trial.PointerState != string(updateengine.StateTrial) || trial.PointerActiveVersion != "v1.1.0" {
		t.Fatalf("trial pointer not surfaced: %#v", trial)
	}

	// 结构非法的 pointer 必须报 invalid，让 Setup 明确失败而不是当成 legacy。
	if err := os.WriteFile(updateStore.ActivePath(), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalid, err := Inspect(root)
	if err != nil {
		t.Fatal(err)
	}
	if invalid.PointerState != "invalid" {
		t.Fatalf("invalid pointer_state=%q, want invalid", invalid.PointerState)
	}
}
