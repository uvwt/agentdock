//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/uvwt/agentdock/internal/fs/processlock"
	processctl "github.com/uvwt/agentdock/internal/process"
	"github.com/uvwt/agentdock/internal/updateengine"
)

const (
	setupRuntimeHostFlag = "--setup-runtime-host"
	taskCoreHostFlag     = "--task-core-host"
)

func main() {
	if len(os.Args) > 1 && strings.EqualFold(strings.TrimSpace(os.Args[1]), taskCoreHostFlag) {
		exitCode, err := runTaskCoreHost(os.Args[2:])
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(exitCode)
	}
	if len(os.Args) > 1 && strings.EqualFold(strings.TrimSpace(os.Args[1]), setupRuntimeHostFlag) {
		exitCode, err := runSetupRuntimeHost(os.Args[2:])
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(exitCode)
	}
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

		var runErr error
		if coreLaunchRequiresParentLifetime(os.Args[1:]) {
			// Scheduled Task owns the stable shim, not the generation Core. Keep the Core
			// in a kill-on-close Job owned by this shim so ending the task cannot orphan it.
			if err := command.Start(); err != nil {
				return fmt.Errorf("start AgentDock active generation: %w", err)
			}
			controller, err := processctl.Attach(command)
			if err != nil {
				_ = command.Process.Kill()
				_ = command.Wait()
				return fmt.Errorf("supervise AgentDock active generation: %w", err)
			}
			runErr = command.Wait()
			closeErr := controller.Close()
			if runErr == nil && closeErr != nil {
				return fmt.Errorf("release AgentDock active generation supervisor: %w", closeErr)
			}
		} else {
			runErr = command.Run()
		}
		if runErr != nil {
			var exitErr *exec.ExitError
			if errors.As(runErr, &exitErr) {
				os.Exit(exitErr.ExitCode())
			}
			return fmt.Errorf("run AgentDock active generation: %w", runErr)
		}
		return nil
	}

	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start AgentDock tray generation: %w", err)
	}
	return command.Process.Release()
}

func coreLaunchRequiresParentLifetime(args []string) bool {
	return len(args) >= 2 &&
		strings.EqualFold(strings.TrimSpace(args[0]), "service") &&
		strings.EqualFold(strings.TrimSpace(args[1]), "launch-core")
}

type installerTrialTransaction struct {
	TransactionID string             `json:"transaction_id"`
	Platform      string             `json:"platform"`
	Action        string             `json:"action"`
	SourceVersion string             `json:"source_version"`
	TargetVersion string             `json:"target_version"`
	State         updateengine.State `json:"state"`
	InstallRoot   string             `json:"install_root"`
}

func installerOwnsActiveTrial(root string, active updateengine.ActiveVersion) (bool, error) {
	data, err := os.ReadFile(filepath.Join(root, "install", "transaction.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read installer transaction: %w", err)
	}
	var transaction installerTrialTransaction
	if err := json.Unmarshal(data, &transaction); err != nil {
		return false, fmt.Errorf("parse installer transaction: %w", err)
	}
	if transaction.Platform != "windows" ||
		(transaction.Action != "install" && transaction.Action != "repair") ||
		transaction.State != updateengine.StateTrial ||
		transaction.TransactionID != active.TransactionID ||
		updateengine.NormalizeVersion(transaction.TargetVersion) != updateengine.NormalizeVersion(active.ActiveVersion) ||
		updateengine.NormalizeVersion(transaction.SourceVersion) != updateengine.NormalizeVersion(active.FallbackVersion) ||
		!sameWindowsPath(transaction.InstallRoot, root) {
		return false, nil
	}

	// Installer 持有这个独占锁贯穿 stage、trial 启动和健康检查。只有活着的事务 owner
	// 才能临时授权 stable shim 路由到未提交 generation；崩溃后锁释放，陈旧 trial 会被拒绝。
	lock, acquired, err := processlock.TryAcquire(filepath.Join(root, "install", "transaction.lock"))
	if err != nil {
		return false, fmt.Errorf("probe installer transaction lock: %w", err)
	}
	if acquired {
		if err := lock.Release(); err != nil {
			return false, fmt.Errorf("release installer transaction probe lock: %w", err)
		}
		return false, nil
	}
	return true, nil
}

func sameWindowsPath(left, right string) bool {
	left, leftErr := filepath.Abs(strings.TrimSpace(left))
	right, rightErr := filepath.Abs(strings.TrimSpace(right))
	return leftErr == nil && rightErr == nil && strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func resolveActiveWithRecovery(root string, store *updateengine.Store, layout updateengine.WindowsLayout) (updateengine.ActiveVersion, error) {
	active, err := store.ReadActive()
	if err != nil {
		return updateengine.ActiveVersion{}, fmt.Errorf("read AgentDock active version: %w", err)
	}

	transaction, transactionErr := store.ReadTransaction()
	if transactionErr != nil {
		if active.State == updateengine.StateTrial {
			if !os.IsNotExist(transactionErr) {
				return updateengine.ActiveVersion{}, fmt.Errorf("read pending update transaction: %w", transactionErr)
			}
			owned, err := installerOwnsActiveTrial(root, active)
			if err != nil {
				return updateengine.ActiveVersion{}, err
			}
			if owned {
				return active, nil
			}
			return updateengine.ActiveVersion{}, errors.New("active generation is still a trial without a live update or installer transaction")
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
