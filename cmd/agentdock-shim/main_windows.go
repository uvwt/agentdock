//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/uvwt/agentdock/internal/fs/processlock"
	"github.com/uvwt/agentdock/internal/updateengine"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve AgentDock stable entry: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("resolve AgentDock stable entry path: %w", err)
	}
	root := filepath.Dir(filepath.Dir(executable))
	store, err := updateengine.NewStore(root)
	if err != nil {
		return err
	}
	layout, err := updateengine.NewWindowsLayout(root)
	if err != nil {
		return err
	}
	active, err := resolveActiveWithRecovery(root, store, layout)
	if err != nil {
		return err
	}

	tray := strings.EqualFold(filepath.Base(executable), updateengine.StableTrayShimName)
	target := layout.GenerationCore(active.ActiveVersion)
	if tray {
		target = layout.GenerationTray(active.ActiveVersion)
	}
	if info, err := os.Stat(target); err != nil || info.IsDir() {
		return fmt.Errorf("AgentDock active generation is incomplete: %s", target)
	}

	command := exec.Command(target, os.Args[1:]...)
	command.Dir = root
	if !tray || trayRequiresWait(os.Args[1:]) {
		command.Stdin = os.Stdin
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				os.Exit(exitErr.ExitCode())
			}
			return fmt.Errorf("run AgentDock active generation: %w", err)
		}
		return nil
	}

	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start AgentDock tray generation: %w", err)
	}
	return command.Process.Release()
}

func resolveActiveWithRecovery(root string, store *updateengine.Store, layout updateengine.WindowsLayout) (updateengine.ActiveVersion, error) {
	active, err := store.ReadActive()
	if err != nil {
		return updateengine.ActiveVersion{}, fmt.Errorf("read AgentDock active version: %w", err)
	}

	transaction, transactionErr := store.ReadTransaction()
	if transactionErr != nil {
		if active.State == updateengine.StateTrial {
			// Installer fresh bootstrap 把 pointer 停在 trial，直到 install commit。
			// shim 恢复只认 update/transaction.json；没有这份 journal 就不能把未完成安装当 committed 启动。
			return updateengine.ActiveVersion{}, fmt.Errorf("active generation is still a trial and no update transaction is present; refusing to launch an uncommitted installer generation: %w", transactionErr)
		}
		return active, nil
	}
	pendingTrial := transaction.State == updateengine.StateTrial || transaction.State == updateengine.StateRollingBack
	if !pendingTrial && active.State != updateengine.StateTrial {
		return active, nil
	}
	if !pendingTrial {
		return updateengine.ActiveVersion{}, fmt.Errorf("active generation is trial but transaction %s is %s", transaction.TransactionID, transaction.State)
	}
	if transaction.Platform != "windows" || transaction.Windows == nil {
		return updateengine.ActiveVersion{}, errors.New("pending Windows generation transaction has no Windows plan")
	}
	if active.State == updateengine.StateTrial && active.TransactionID != transaction.TransactionID {
		return updateengine.ActiveVersion{}, fmt.Errorf("active trial transaction %s does not match journal %s", active.TransactionID, transaction.TransactionID)
	}

	// The Arbiter journals state=trial before it swaps the active pointer. A crash can therefore
	// leave either (a) active=trial target or (b) active=committed source with a pending trial
	// journal. In both cases a held OS lock means the original source Arbiter is still alive;
	// once the kernel releases the lock, the source known-good Arbiter performs conservative rollback.
	lockPath := filepath.Join(root, "update", "transaction.lock")
	lock, acquired, err := processlock.TryAcquire(lockPath)
	if err != nil {
		return updateengine.ActiveVersion{}, fmt.Errorf("probe update transaction lock: %w", err)
	}
	if !acquired {
		return active, nil
	}
	if err := lock.Release(); err != nil {
		return updateengine.ActiveVersion{}, fmt.Errorf("release update transaction probe lock: %w", err)
	}

	sourceVersion := updateengine.NormalizeVersion(transaction.SourceVersion)
	if sourceVersion == "" {
		return updateengine.ActiveVersion{}, errors.New("interrupted update trial has no source generation")
	}
	arbiterPath := layout.GenerationArbiter(sourceVersion)
	command := exec.Command(arbiterPath, "--root", root, "--transaction-id", transaction.TransactionID)
	command.Dir = root
	var recoveryStdout, recoveryStderr bytes.Buffer
	command.Stdout = &recoveryStdout
	command.Stderr = &recoveryStderr
	runErr := command.Run()
	result, resultErr := store.ReadResult(transaction.TransactionID)
	if resultErr != nil || (result.State != updateengine.StateRolledBack && result.State != updateengine.StateCommitted) {
		if runErr != nil {
			details := strings.TrimSpace(recoveryStderr.String())
			if details == "" {
				details = strings.TrimSpace(recoveryStdout.String())
			}
			if details != "" {
				return updateengine.ActiveVersion{}, fmt.Errorf("recover interrupted AgentDock update: %w: %s", runErr, details)
			}
			return updateengine.ActiveVersion{}, fmt.Errorf("recover interrupted AgentDock update: %w", runErr)
		}
		return updateengine.ActiveVersion{}, fmt.Errorf("recover interrupted AgentDock update did not reach a safe terminal result: %v", resultErr)
	}
	recovered, err := store.ReadActive()
	if err != nil {
		return updateengine.ActiveVersion{}, fmt.Errorf("read recovered AgentDock active version: %w", err)
	}
	return recovered, nil
}

func trayRequiresWait(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, arg := range args {
		if !strings.EqualFold(strings.TrimSpace(arg), "--background") {
			return true
		}
	}
	return false
}
