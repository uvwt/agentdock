package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/fs/processlock"
	"github.com/uvwt/agentdock/internal/updateengine"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("agentdock-arbiter", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	root := flags.String("root", "", "AgentDock install root")
	transactionID := flags.String("transaction-id", "", "update transaction id")
	recoverIfUnlocked := flags.Bool("recover-if-unlocked", false, "recover only when no live arbiter owns the transaction lock")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*root) == "" || strings.TrimSpace(*transactionID) == "" {
		return errors.New("usage: agentdock-arbiter --root <install-root> --transaction-id <id>")
	}

	store, err := updateengine.NewStore(*root)
	if err != nil {
		return err
	}
	transaction, err := store.ReadTransaction()
	if err != nil {
		return fmt.Errorf("read update transaction before arbitration: %w", err)
	}
	if transaction.TransactionID != strings.TrimSpace(*transactionID) {
		return fmt.Errorf("update transaction changed: got %s, want %s", transaction.TransactionID, strings.TrimSpace(*transactionID))
	}
	if err := validateKnownGoodSource(transaction); err != nil {
		return err
	}
	if *recoverIfUnlocked {
		lockPath := filepath.Join(store.Root(), "update", "transaction.lock")
		lock, acquired, err := processlock.TryAcquire(lockPath)
		if err != nil {
			return err
		}
		if !acquired {
			// The original source Arbiter still owns the transaction. A newly launched trial App
			// probes recovery on every startup, so this is the normal live-update path.
			return nil
		}
		if err := lock.Release(); err != nil {
			return err
		}
	}

	driver, err := newPlatformDriver(*root)
	if err != nil {
		return err
	}
	arbiter := updateengine.Arbiter{Store: store, Driver: driver}
	arbiterCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	result, runErr := arbiter.Run(arbiterCtx, transaction.TransactionID)
	if result.TransactionID != "" {
		_ = json.NewEncoder(os.Stdout).Encode(result)
	}
	return runErr
}

func validateKnownGoodSource(transaction updateengine.Transaction) error {
	if transaction.Platform == "windows" && transaction.Windows != nil && transaction.Windows.BootstrapMigration {
		return nil
	}
	if transaction.Platform == "darwin" && transaction.MacOS != nil && transaction.MacOS.BootstrapMigration {
		return nil
	}
	current := updateengine.NormalizeVersion(buildinfo.Version)
	if current == "" || current != updateengine.NormalizeVersion(transaction.SourceVersion) {
		return fmt.Errorf("arbiter version %s is not the known-good source version %s", buildinfo.Version, transaction.SourceVersion)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve arbiter executable: %w", err)
	}
	executable, _ = filepath.Abs(executable)
	expected := expectedSourceArbiter(transaction)
	if expected != "" && !samePath(executable, expected) {
		return fmt.Errorf("arbiter executable %s is outside the known-good source generation %s", executable, expected)
	}
	return nil
}

func samePath(a, b string) bool {
	a, errA := filepath.Abs(a)
	b, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
