package updateengine

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

type recordingDriver struct {
	calls              []string
	verifyError        error
	rollbackErr        error
	warnings           []string
	cancelDuringVerify context.CancelFunc
	rollbackContextErr error
}

func (driver *recordingDriver) PrepareTrial(context.Context, Transaction) error {
	driver.calls = append(driver.calls, "prepare")
	return nil
}
func (driver *recordingDriver) VerifyTrial(context.Context, Transaction) ([]string, error) {
	driver.calls = append(driver.calls, "verify")
	if driver.cancelDuringVerify != nil {
		driver.cancelDuringVerify()
	}
	return driver.warnings, driver.verifyError
}
func (driver *recordingDriver) Commit(context.Context, Transaction) error {
	driver.calls = append(driver.calls, "commit")
	return nil
}
func (driver *recordingDriver) Rollback(ctx context.Context, _ Transaction) error {
	driver.calls = append(driver.calls, "rollback")
	driver.rollbackContextErr = ctx.Err()
	return driver.rollbackErr
}

func writeWindowsTransaction(t *testing.T, state State) (*Store, Transaction) {
	t.Helper()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := NewTransaction("windows", "v0.8.3", "v0.9.0")
	if err != nil {
		t.Fatal(err)
	}
	transaction.State = state
	transaction.ActiveVersion = "v0.8.3"
	transaction.FallbackVersion = "v0.8.2"
	transaction.Windows = &WindowsPlan{
		InstallRoot:      root,
		SourceGeneration: filepath.Join(root, "versions", "v0.8.3"),
		TargetGeneration: filepath.Join(root, "versions", "v0.9.0"),
	}
	if err := store.WriteTransaction(transaction); err != nil {
		t.Fatal(err)
	}
	return store, transaction
}

func TestArbiterCommitsVerifiedTrial(t *testing.T) {
	store, transaction := writeWindowsTransaction(t, StateStaged)
	driver := &recordingDriver{warnings: []string{"requires approval"}}
	result, err := (Arbiter{Store: store, Driver: driver}).Run(context.Background(), transaction.TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != StateCommitted || result.ActiveVersion != "v0.9.0" || result.FallbackVersion != "v0.8.3" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !reflect.DeepEqual(driver.calls, []string{"prepare", "verify", "commit"}) {
		t.Fatalf("calls = %v", driver.calls)
	}
	if !reflect.DeepEqual(result.Warnings, []string{"requires approval"}) {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestArbiterRollsBackFailedTrial(t *testing.T) {
	store, transaction := writeWindowsTransaction(t, StateStaged)
	driver := &recordingDriver{verifyError: errors.New("wrong version")}
	result, err := (Arbiter{Store: store, Driver: driver}).Run(context.Background(), transaction.TransactionID)
	if err == nil || result.State != StateRolledBack {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !reflect.DeepEqual(driver.calls, []string{"prepare", "verify", "rollback"}) {
		t.Fatalf("calls = %v", driver.calls)
	}
}

func TestArbiterRollbackGetsFreshBudgetAfterTrialContextIsCanceled(t *testing.T) {
	store, transaction := writeWindowsTransaction(t, StateStaged)
	ctx, cancel := context.WithCancel(context.Background())
	driver := &recordingDriver{
		verifyError:        context.Canceled,
		cancelDuringVerify: cancel,
	}

	result, err := (Arbiter{Store: store, Driver: driver}).Run(ctx, transaction.TransactionID)
	if err == nil || result.State != StateRolledBack {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if driver.rollbackContextErr != nil {
		t.Fatalf("rollback inherited canceled trial context: %v", driver.rollbackContextErr)
	}
	if !reflect.DeepEqual(driver.calls, []string{"prepare", "verify", "rollback"}) {
		t.Fatalf("calls = %v", driver.calls)
	}
}

func TestArbiterRecoversInterruptedTrialByRollingBack(t *testing.T) {
	store, transaction := writeWindowsTransaction(t, StateTrial)
	driver := &recordingDriver{}
	result, err := (Arbiter{Store: store, Driver: driver}).Run(context.Background(), transaction.TransactionID)
	if err == nil || result.State != StateRolledBack {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !reflect.DeepEqual(driver.calls, []string{"rollback"}) {
		t.Fatalf("calls = %v", driver.calls)
	}
}
