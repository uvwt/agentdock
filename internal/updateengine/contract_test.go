package updateengine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistsTransactionActiveAndResult(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := NewTransaction("windows", "0.8.3", "0.9.0")
	if err != nil {
		t.Fatal(err)
	}
	transaction.ActiveVersion = "v0.8.3"
	transaction.FallbackVersion = "v0.8.2"
	transaction.Windows = &WindowsPlan{InstallRoot: root, SourceGeneration: filepath.Join(root, "versions", "v0.8.3"), TargetGeneration: filepath.Join(root, "versions", "v0.9.0"), ProgressUIHandoff: true}
	if err := store.WriteTransaction(transaction); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.ReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TransactionID != transaction.TransactionID {
		t.Fatalf("transaction id = %s", loaded.TransactionID)
	}
	if loaded.Windows == nil || !loaded.Windows.ProgressUIHandoff {
		t.Fatal("windows progress UI handoff flag was not persisted")
	}
	active := ActiveVersion{SchemaVersion: SchemaVersion, ActiveVersion: "v0.9.0", FallbackVersion: "v0.8.3", State: StateTrial, TransactionID: transaction.TransactionID, UpdatedAt: time.Now().UTC()}
	if err := store.WriteActive(active); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadActive(); err != nil {
		t.Fatal(err)
	}
	transaction.State = StateTrial
	transaction.Phase = PhaseHealth
	transaction.ActiveVersion = "v0.9.0"
	transaction.FallbackVersion = "v0.8.3"
	result, err := store.Complete(transaction, StateCommitted, nil, []string{"warning"})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != StateCommitted {
		t.Fatalf("state = %s", result.State)
	}
	if _, err := store.ReadResult(transaction.TransactionID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.CurrentResultPath()); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRepairsTerminalResultProjectionsFromJournal(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := NewTransaction("windows", "0.8.3", "0.9.0")
	if err != nil {
		t.Fatal(err)
	}
	transaction.Windows = &WindowsPlan{
		InstallRoot:      root,
		SourceGeneration: filepath.Join(root, "versions", "v0.8.3"),
		TargetGeneration: filepath.Join(root, "versions", "v0.9.0"),
	}
	transaction.State = StateTrial
	transaction.Phase = PhaseHealth
	transaction.ActiveVersion = "v0.9.0"
	transaction.FallbackVersion = "v0.8.3"
	if _, err := store.Complete(transaction, StateCommitted, nil, []string{"warning"}); err != nil {
		t.Fatal(err)
	}

	// transaction.json is the commit point. Simulate power loss before either projection
	// becomes durable by deleting both result files while leaving the terminal journal intact.
	if err := os.Remove(store.ResultPath(transaction.TransactionID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.CurrentResultPath()); err != nil {
		t.Fatal(err)
	}
	result, err := store.ReadResult(transaction.TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != StateCommitted || result.ActiveVersion != "v0.9.0" || len(result.Warnings) != 1 {
		t.Fatalf("repaired result = %+v", result)
	}
	if _, err := os.Stat(store.CurrentResultPath()); err != nil {
		t.Fatalf("current result projection was not repaired: %v", err)
	}

	// The per-transaction result may reach disk before result.json. Reading the durable
	// result must also restore the current projection used by desktop clients.
	if err := os.Remove(store.CurrentResultPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadResult(transaction.TransactionID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.CurrentResultPath()); err != nil {
		t.Fatalf("current result projection was not repaired from per-transaction result: %v", err)
	}
}

func TestWindowsLayoutRecognizesGenerationBinaries(t *testing.T) {
	layout, err := NewWindowsLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !layout.IsGenerationBinary(layout.GenerationCore("0.9.0")) {
		t.Fatal("core generation not recognized")
	}
	if layout.IsGenerationBinary(layout.CoreShim()) {
		t.Fatal("stable shim recognized as generation")
	}
}
