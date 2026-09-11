package updateengine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/uvwt/agentdock/internal/fs/processlock"
)

type Driver interface {
	PrepareTrial(context.Context, Transaction) error
	VerifyTrial(context.Context, Transaction) ([]string, error)
	Commit(context.Context, Transaction) error
	Rollback(context.Context, Transaction) error
}

type Arbiter struct {
	Store  *Store
	Driver Driver
}

const rollbackRecoveryTimeout = 3 * time.Minute

func (arbiter Arbiter) Run(ctx context.Context, transactionID string) (Result, error) {
	if arbiter.Store == nil || arbiter.Driver == nil {
		return Result{}, errors.New("update arbiter requires store and platform driver")
	}
	if transactionID == "" {
		return Result{}, errors.New("update arbiter transaction id is required")
	}

	lockPath := filepath.Join(arbiter.Store.Root(), "update", "transaction.lock")
	lock, err := processlock.Acquire(ctx, lockPath)
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()

	transaction, err := arbiter.Store.ReadTransaction()
	if err != nil {
		return Result{}, fmt.Errorf("read update transaction: %w", err)
	}
	if transaction.TransactionID != transactionID {
		return Result{}, fmt.Errorf("update transaction changed: got %s, want %s", transaction.TransactionID, transactionID)
	}

	switch transaction.State {
	case StateCommitted, StateRolledBack, StateFailed:
		return arbiter.Store.ReadResult(transactionID)
	case StateTrial, StateRollingBack:
		return arbiter.recoverInterruptedTrial(ctx, transaction)
	case StateStaged:
		return arbiter.runTrial(ctx, transaction)
	default:
		return Result{}, fmt.Errorf("cannot arbitrate update state %s", transaction.State)
	}
}

func (arbiter Arbiter) runTrial(ctx context.Context, transaction Transaction) (Result, error) {
	transaction.State = StateTrial
	transaction.Phase = PhaseActivate
	transaction.Failure = nil
	if err := arbiter.Store.WriteTransaction(transaction); err != nil {
		return Result{}, err
	}

	if err := arbiter.Driver.PrepareTrial(ctx, transaction); err != nil {
		return arbiter.rollback(ctx, transaction, "trial-activation-failed", err)
	}

	transaction.Phase = PhaseHealth
	if err := arbiter.Store.WriteTransaction(transaction); err != nil {
		return arbiter.rollback(ctx, transaction, "trial-journal-failed", err)
	}
	warnings, err := arbiter.Driver.VerifyTrial(ctx, transaction)
	if err != nil {
		return arbiter.rollback(ctx, transaction, "trial-verification-failed", err)
	}

	transaction.Phase = PhaseCommit
	transaction.Warnings = append([]string(nil), warnings...)
	if err := arbiter.Store.WriteTransaction(transaction); err != nil {
		return arbiter.rollback(ctx, transaction, "commit-journal-failed", err)
	}
	if err := arbiter.Driver.Commit(ctx, transaction); err != nil {
		return arbiter.rollback(ctx, transaction, "commit-failed", err)
	}
	transaction.ActiveVersion = transaction.TargetVersion
	transaction.FallbackVersion = transaction.SourceVersion
	result, err := arbiter.Store.Complete(transaction, StateCommitted, nil, warnings)
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

func (arbiter Arbiter) recoverInterruptedTrial(ctx context.Context, transaction Transaction) (Result, error) {
	cause := errors.New("previous update trial was interrupted before commit")
	return arbiter.rollback(ctx, transaction, "trial-interrupted", cause)
}

func (arbiter Arbiter) rollback(ctx context.Context, transaction Transaction, code string, cause error) (Result, error) {
	failure := &Failure{Code: code, Message: cause.Error(), At: time.Now().UTC()}
	transaction.State = StateRollingBack
	transaction.Phase = PhaseRollback
	transaction.Failure = failure
	if err := arbiter.Store.WriteTransaction(transaction); err != nil {
		return Result{}, errors.Join(cause, fmt.Errorf("persist rollback journal: %w", err))
	}
	// 一旦进入 trial，恢复已知良好版本就是独立的安全事务，不能继续消耗 trial 的
	// deadline/cancellation。否则 trial 在超时边缘失败时，rollback 会拿到已经取消的
	// context，导致“物理上已换回旧版本、journal 却记为 failed”的半恢复状态。
	// 仍给 rollback 自己的有限预算，避免平台恢复逻辑无限挂起。
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackRecoveryTimeout)
	defer cancel()
	if err := arbiter.Driver.Rollback(rollbackCtx, transaction); err != nil {
		rollbackFailure := &Failure{
			Code:    "rollback-failed",
			Message: errors.Join(cause, err).Error(),
			At:      time.Now().UTC(),
		}
		transaction.ActiveVersion = transaction.SourceVersion
		transaction.FallbackVersion = transaction.SourceVersion
		result, resultErr := arbiter.Store.Complete(transaction, StateFailed, rollbackFailure, transaction.Warnings)
		if resultErr != nil {
			return Result{}, errors.Join(cause, err, resultErr)
		}
		return result, errors.Join(cause, fmt.Errorf("update rollback failed: %w", err))
	}
	transaction.ActiveVersion = transaction.SourceVersion
	transaction.FallbackVersion = transaction.SourceVersion
	result, err := arbiter.Store.Complete(transaction, StateRolledBack, failure, transaction.Warnings)
	if err != nil {
		return Result{}, errors.Join(cause, err)
	}
	return result, cause
}
