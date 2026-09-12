package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
	"github.com/uvwt/agentdock/internal/updateengine"
)

type Store struct {
	root string
}

func NewStore(root string) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("install store root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve install store root: %w", err)
	}
	return &Store{root: filepath.Clean(absolute)}, nil
}

func (store *Store) Root() string { return store.root }

func (store *Store) TransactionPath() string {
	return filepath.Join(store.root, "install", "transaction.json")
}

func (store *Store) ResultPath(transactionID string) string {
	return filepath.Join(store.root, "install", "results", transactionID+".json")
}

func (store *Store) CurrentResultPath() string {
	return filepath.Join(store.root, "install", "result.json")
}

func (store *Store) LockPath() string {
	return filepath.Join(store.root, "install", "transaction.lock")
}

func (store *Store) WriteTransaction(transaction Transaction) error {
	transaction.UpdatedAt = time.Now().UTC()
	return writeJSON(store.TransactionPath(), transaction)
}

func (store *Store) ReadTransaction() (Transaction, error) {
	var transaction Transaction
	if err := readJSON(store.TransactionPath(), &transaction); err != nil {
		return Transaction{}, err
	}
	return transaction, nil
}

func (store *Store) WriteResult(result Result) error {
	if err := writeJSON(store.ResultPath(result.TransactionID), result); err != nil {
		return err
	}
	return writeJSON(store.CurrentResultPath(), result)
}

func (store *Store) ReadCurrentResult() (Result, error) {
	var result Result
	if err := readJSON(store.CurrentResultPath(), &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (store *Store) ReadResult(transactionID string) (Result, error) {
	transactionID = strings.TrimSpace(transactionID)
	if transactionID == "" {
		return Result{}, errors.New("install result transaction id is required")
	}
	var result Result
	if err := readJSON(store.ResultPath(transactionID), &result); err == nil {
		if result.TransactionID != transactionID {
			return Result{}, fmt.Errorf("install result transaction changed: got %s, want %s", result.TransactionID, transactionID)
		}
		if err := store.repairCurrentResultProjection(result); err != nil {
			return Result{}, err
		}
		return result, nil
	}

	// transaction.json 是权威事务。崩溃可能发生在 per-id result 或 current projection 写完之前。
	transaction, err := store.ReadTransaction()
	if err != nil || transaction.TransactionID != transactionID {
		return Result{}, fmt.Errorf("install result %s 不存在", transactionID)
	}
	result = resultFromTransaction(transaction)
	if transaction.State == updateengine.StateCommitted || transaction.State == updateengine.StateRolledBack || transaction.State == updateengine.StateFailed {
		if err := store.WriteResult(result); err != nil {
			return Result{}, fmt.Errorf("repair terminal install result: %w", err)
		}
	}
	return result, nil
}

func resultFromTransaction(transaction Transaction) Result {
	result := Result{
		SchemaVersion:   SchemaVersion,
		TransactionID:   transaction.TransactionID,
		Platform:        transaction.Platform,
		Action:          transaction.Action,
		State:           transaction.State,
		Phase:           transaction.Phase,
		Version:         transaction.TargetVersion,
		ActiveVersion:   transaction.ActiveVersion,
		FallbackVersion: transaction.FallbackVersion,
		Failure:         transaction.Failure,
		Warnings:        append([]string(nil), transaction.Warnings...),
		StartedAt:       transaction.StartedAt,
	}
	if transaction.CompletedAt != nil {
		result.CompletedAt = *transaction.CompletedAt
	}
	return result
}

func (store *Store) repairCurrentResultProjection(result Result) error {
	transaction, err := store.ReadTransaction()
	if err != nil || transaction.TransactionID != result.TransactionID {
		return nil
	}
	var current Result
	if err := readJSON(store.CurrentResultPath(), &current); err == nil && current.TransactionID == result.TransactionID {
		return nil
	}
	if err := writeJSON(store.CurrentResultPath(), result); err != nil {
		return fmt.Errorf("repair current install result projection: %w", err)
	}
	return nil
}

func (store *Store) Complete(transaction Transaction, state updateengine.State, result Result) (Result, error) {
	now := time.Now().UTC()
	transaction.State = state
	transaction.CompletedAt = &now
	transaction.Failure = result.Failure
	transaction.Warnings = append([]string(nil), result.Warnings...)
	switch state {
	case updateengine.StateCommitted:
		transaction.Phase = PhaseCommit
		if result.ActiveVersion != "" {
			transaction.ActiveVersion = result.ActiveVersion
		}
		if result.FallbackVersion != "" {
			transaction.FallbackVersion = result.FallbackVersion
		}
	case updateengine.StateRolledBack:
		// abandon / 成功回滚后不能保留 phase=commit，否则看起来像“提交了又撤回”。
		// ActiveVersion 允许空：fresh install 被撤回后，失败目标不能当成下一次 source。
		transaction.Phase = PhaseRollback
		transaction.ActiveVersion = result.ActiveVersion
		transaction.FallbackVersion = result.FallbackVersion
	default:
		if transaction.Phase == "" {
			transaction.Phase = PhaseRollback
		}
		transaction.ActiveVersion = result.ActiveVersion
		transaction.FallbackVersion = result.FallbackVersion
	}
	if err := store.WriteTransaction(transaction); err != nil {
		return Result{}, err
	}
	result.SchemaVersion = SchemaVersion
	result.TransactionID = transaction.TransactionID
	result.Platform = transaction.Platform
	result.Action = transaction.Action
	result.State = state
	result.Phase = transaction.Phase
	result.StartedAt = transaction.StartedAt
	result.CompletedAt = now
	if result.Version == "" {
		result.Version = transaction.TargetVersion
	}
	if err := store.WriteResult(result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode install state: %w", err)
	}
	data = append(data, '\n')
	if err := atomicfile.Write(path, data, 0o600); err != nil {
		return fmt.Errorf("persist install state %s: %w", path, err)
	}
	return nil
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("parse install state %s: %w", path, err)
	}
	return nil
}
