package updateengine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
)

type Store struct {
	root string
}

func NewStore(root string) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("update store root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve update store root: %w", err)
	}
	return &Store{root: filepath.Clean(absolute)}, nil
}

func (store *Store) Root() string { return store.root }

func (store *Store) ActivePath() string {
	return filepath.Join(store.root, "active-version.json")
}

func (store *Store) TransactionPath() string {
	return filepath.Join(store.root, "update", "transaction.json")
}

func (store *Store) ResultPath(transactionID string) string {
	return filepath.Join(store.root, "update", "results", transactionID+".json")
}

func (store *Store) CurrentResultPath() string {
	return filepath.Join(store.root, "update", "result.json")
}

func (store *Store) WriteTransaction(transaction Transaction) error {
	transaction.UpdatedAt = time.Now().UTC()
	if err := transaction.Validate(); err != nil {
		return err
	}
	return writeJSON(store.TransactionPath(), transaction)
}

func (store *Store) ReadTransaction() (Transaction, error) {
	var transaction Transaction
	if err := readJSON(store.TransactionPath(), &transaction); err != nil {
		return Transaction{}, err
	}
	if err := transaction.Validate(); err != nil {
		return Transaction{}, err
	}
	return transaction, nil
}

func (store *Store) WriteResult(result Result) error {
	if err := result.Validate(); err != nil {
		return err
	}
	if err := writeJSON(store.ResultPath(result.TransactionID), result); err != nil {
		return err
	}
	return writeJSON(store.CurrentResultPath(), result)
}

func (store *Store) ReadResult(transactionID string) (Result, error) {
	var result Result
	resultErr := readJSON(store.ResultPath(transactionID), &result)
	if resultErr == nil {
		if err := result.Validate(); err != nil {
			resultErr = err
		} else if result.TransactionID != transactionID {
			resultErr = fmt.Errorf("update result transaction changed: got %s, want %s", result.TransactionID, transactionID)
		} else {
			if err := store.repairCurrentResultProjection(result); err != nil {
				return Result{}, err
			}
			return result, nil
		}
	}

	// transaction.json is the durable commit point. A crash can happen after the terminal
	// journal is synced but before either result projection is written. Reconstructing the
	// projection from that journal makes terminal success/rollback observable after restart
	// without ever guessing from the active process state.
	transaction, transactionErr := store.ReadTransaction()
	if transactionErr != nil || transaction.TransactionID != transactionID {
		return Result{}, resultErr
	}
	result, err := resultFromTerminalTransaction(transaction)
	if err != nil {
		return Result{}, resultErr
	}
	if err := store.WriteResult(result); err != nil {
		return Result{}, fmt.Errorf("repair terminal update result: %w", err)
	}
	return result, nil
}

func resultFromTerminalTransaction(transaction Transaction) (Result, error) {
	if transaction.State != StateCommitted && transaction.State != StateRolledBack && transaction.State != StateFailed {
		return Result{}, fmt.Errorf("update transaction is not terminal: %s", transaction.State)
	}
	if transaction.CompletedAt == nil || transaction.CompletedAt.IsZero() {
		return Result{}, errors.New("terminal update transaction has no completion timestamp")
	}
	result := Result{
		SchemaVersion:   SchemaVersion,
		TransactionID:   transaction.TransactionID,
		Platform:        transaction.Platform,
		SourceVersion:   transaction.SourceVersion,
		TargetVersion:   transaction.TargetVersion,
		ActiveVersion:   transaction.ActiveVersion,
		FallbackVersion: transaction.FallbackVersion,
		State:           transaction.State,
		CompletedAt:     *transaction.CompletedAt,
		Failure:         transaction.Failure,
		Warnings:        append([]string(nil), transaction.Warnings...),
	}
	if err := result.Validate(); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (store *Store) repairCurrentResultProjection(result Result) error {
	transaction, err := store.ReadTransaction()
	if err != nil || transaction.TransactionID != result.TransactionID {
		// Historical per-transaction results must never replace the projection for a newer
		// transaction. If there is no matching current journal, the historical read is done.
		return nil
	}
	if _, err := resultFromTerminalTransaction(transaction); err != nil {
		return nil
	}

	var current Result
	if err := readJSON(store.CurrentResultPath(), &current); err == nil &&
		current.Validate() == nil &&
		current.TransactionID == result.TransactionID {
		return nil
	}
	if err := writeJSON(store.CurrentResultPath(), result); err != nil {
		return fmt.Errorf("repair current update result projection: %w", err)
	}
	return nil
}

func (store *Store) WriteActive(active ActiveVersion) error {
	active.UpdatedAt = time.Now().UTC()
	if err := active.Validate(); err != nil {
		return err
	}
	return writeJSON(store.ActivePath(), active)
}

func (store *Store) ReadActive() (ActiveVersion, error) {
	var active ActiveVersion
	if err := readJSON(store.ActivePath(), &active); err != nil {
		return ActiveVersion{}, err
	}
	if err := active.Validate(); err != nil {
		return ActiveVersion{}, err
	}
	return active, nil
}

func (store *Store) Complete(transaction Transaction, state State, failure *Failure, warnings []string) (Result, error) {
	if state != StateCommitted && state != StateRolledBack && state != StateFailed {
		return Result{}, fmt.Errorf("cannot complete update transaction with non-terminal state %s", state)
	}
	now := time.Now().UTC()
	transaction.State = state
	transaction.CompletedAt = &now
	transaction.Failure = failure
	transaction.Warnings = append([]string(nil), warnings...)
	if state == StateCommitted {
		transaction.Phase = PhaseCommit
	} else {
		transaction.Phase = PhaseRollback
	}
	if err := store.WriteTransaction(transaction); err != nil {
		return Result{}, err
	}
	result := Result{
		SchemaVersion:   SchemaVersion,
		TransactionID:   transaction.TransactionID,
		Platform:        transaction.Platform,
		SourceVersion:   transaction.SourceVersion,
		TargetVersion:   transaction.TargetVersion,
		ActiveVersion:   transaction.ActiveVersion,
		FallbackVersion: transaction.FallbackVersion,
		State:           state,
		CompletedAt:     now,
		Failure:         failure,
		Warnings:        append([]string(nil), warnings...),
	}
	if err := store.WriteResult(result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode update state: %w", err)
	}
	data = append(data, '\n')
	if err := atomicfile.Write(path, data, 0o600); err != nil {
		return fmt.Errorf("persist update state %s: %w", path, err)
	}
	return nil
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("parse update state %s: %w", path, err)
	}
	return nil
}
