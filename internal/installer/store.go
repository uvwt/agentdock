package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

// ReadAuthoritativeResult 返回当前权威事务对应的结果。terminal transaction 是 durable commit point；
// 即使进程死在 transaction.json 与 result projection 两次原子写之间，读取也必须收敛到终态。
func (store *Store) ReadAuthoritativeResult() (Result, error) {
	transaction, err := store.ReadTransaction()
	if err == nil && isTerminalInstallState(transaction.State) {
		return store.ReadResult(transaction.TransactionID)
	}
	return store.ReadCurrentResult()
}

func (store *Store) ReadResult(transactionID string) (Result, error) {
	transactionID = strings.TrimSpace(transactionID)
	if transactionID == "" {
		return Result{}, errors.New("install result transaction id is required")
	}

	var persisted Result
	persistedErr := readJSON(store.ResultPath(transactionID), &persisted)
	if persistedErr == nil && persisted.TransactionID != transactionID {
		return Result{}, fmt.Errorf("install result transaction changed: got %s, want %s", persisted.TransactionID, transactionID)
	}

	transaction, transactionErr := store.ReadTransaction()
	if transactionErr == nil && transaction.TransactionID == transactionID && isTerminalInstallState(transaction.State) {
		// terminal transaction 已经对外生效；projection 只能从它向前收敛，不能继续相信同事务的 trial 结果。
		result := resultFromTransaction(transaction)
		if persistedErr == nil && transaction.State == updateengine.StateCommitted && transaction.Action != ActionUninstall {
			// commit 前的 trial 已完成健康检查，保留这些非状态机展示字段；rollback/failed 则故意清空，
			// 防止失败 target 的 URL/健康状态重新泄漏到最终 projection。
			result.LocalMCPURL = persisted.LocalMCPURL
			result.PublicURL = persisted.PublicURL
			result.Healthy = persisted.Healthy
			result.PrivilegeMode = persisted.PrivilegeMode
		}
		if persistedErr != nil || !reflect.DeepEqual(persisted, result) {
			if err := store.WriteResult(result); err != nil {
				return Result{}, fmt.Errorf("repair terminal install result: %w", err)
			}
		} else if err := store.repairCurrentResultProjection(result); err != nil {
			return Result{}, err
		}
		return result, nil
	}

	if persistedErr == nil {
		if err := store.repairCurrentResultProjection(persisted); err != nil {
			return Result{}, err
		}
		return persisted, nil
	}
	if transactionErr != nil || transaction.TransactionID != transactionID {
		return Result{}, fmt.Errorf("install result %s 不存在", transactionID)
	}
	return resultFromTransaction(transaction), nil
}

func isTerminalInstallState(state updateengine.State) bool {
	return state == updateengine.StateCommitted || state == updateengine.StateRolledBack || state == updateengine.StateFailed
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
	if err := readJSON(store.CurrentResultPath(), &current); err == nil && reflect.DeepEqual(current, result) {
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
