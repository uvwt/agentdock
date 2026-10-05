package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/desktopruntime"
	"github.com/uvwt/agentdock/internal/fs/processlock"
	"github.com/uvwt/agentdock/internal/updateengine"
)

func TestArbiterRunTimeoutPreservesWindowsTrialHeadroom(t *testing.T) {
	// 保守按各阶段有界等待相加：Tunnel 停止 30s、Core 停止 20s、Tray 停止 15s、
	// Core 启动健康预算 60s、版本确认同一预算 60s、Tray 存活确认 15s。
	// 成功路径不会把两段健康预算都耗满，但总事务预算仍应覆盖这个悲观上界。
	const windowsTrialWorstCase = 30*time.Second +
		20*time.Second +
		15*time.Second +
		2*desktopruntime.WindowsCoreStartTimeout +
		15*time.Second
	if arbiterRunTimeout <= windowsTrialWorstCase {
		t.Fatalf("arbiterRunTimeout = %s, must exceed Windows trial worst case %s", arbiterRunTimeout, windowsTrialWorstCase)
	}
}

func TestRecoverIfUnlockedTreatsHeldLockAsHealthyLiveTransaction(t *testing.T) {
	root := t.TempDir()
	store, err := updateengine.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := updateengine.NewTransaction("darwin", "v1.0.0", "v1.0.1")
	if err != nil {
		t.Fatal(err)
	}
	transaction.State = updateengine.StateTrial
	transaction.Phase = updateengine.PhaseHealth
	transaction.MacOS = &updateengine.MacOSPlan{BootstrapMigration: true}
	if err := store.WriteTransaction(transaction); err != nil {
		t.Fatal(err)
	}

	lock, acquired, err := processlock.TryAcquire(filepath.Join(root, "update", "transaction.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("expected test to acquire transaction lock")
	}
	defer lock.Release()

	err = run(context.Background(), []string{
		"--root", root,
		"--transaction-id", transaction.TransactionID,
		"--recover-if-unlocked",
	})
	if err != nil {
		t.Fatalf("recover-if-unlocked with a live owner returned error: %v", err)
	}

	current, err := store.ReadTransaction()
	if err != nil {
		t.Fatal(err)
	}
	if current.State != updateengine.StateTrial {
		t.Fatalf("transaction state = %s, want %s", current.State, updateengine.StateTrial)
	}
}
