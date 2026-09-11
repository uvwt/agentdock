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
	if err := readJSON(store.ResultPath(transactionID), &result); err != nil {
		return Result{}, err
	}
	if err := result.Validate(); err != nil {
		return Result{}, err
	}
	return result, nil
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
