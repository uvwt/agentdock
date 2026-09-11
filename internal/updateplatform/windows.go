//go:build windows

package updateplatform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/desktopruntime"
	"github.com/uvwt/agentdock/internal/updateengine"
)

type WindowsDriver struct {
	root   string
	store  *updateengine.Store
	layout updateengine.WindowsLayout
}

func NewWindowsDriver(root string) (*WindowsDriver, error) {
	layout, err := updateengine.NewWindowsLayout(root)
	if err != nil {
		return nil, err
	}
	store, err := updateengine.NewStore(root)
	if err != nil {
		return nil, err
	}
	return &WindowsDriver{root: layout.Root, store: store, layout: layout}, nil
}

func (driver *WindowsDriver) PrepareTrial(ctx context.Context, transaction updateengine.Transaction) error {
	plan, err := driver.plan(transaction)
	if err != nil {
		return err
	}
	if err := driver.verifyGeneration(transaction.SourceVersion, plan.SourceGeneration); err != nil {
		return fmt.Errorf("source generation is not usable: %w", err)
	}
	if err := driver.verifyGeneration(transaction.TargetVersion, plan.TargetGeneration); err != nil {
		return fmt.Errorf("target generation is not usable: %w", err)
	}

	// 所有停止动作都仍通过 source generation 执行。只有旧 Core/Tunnel/Tray
	// 完整退出后才切 active pointer，避免同一端口被两个 generation 竞争。
	if plan.TunnelWasRunning {
		if err := driver.runStableCore(ctx, "tunnel", "stop", "--runtime-root", driver.root); err != nil {
			return fmt.Errorf("stop source tunnel: %w", err)
		}
	}
	if err := driver.runStableCore(ctx, "service", "stop", "--runtime-root", driver.root); err != nil {
		return fmt.Errorf("stop source core: %w", err)
	}
	if err := desktopruntime.StopBinaryProcesses(ctx, driver.layout.GenerationTray(transaction.SourceVersion), 15*time.Second); err != nil {
		return fmt.Errorf("stop source tray: %w", err)
	}

	active := updateengine.ActiveVersion{
		SchemaVersion:   updateengine.SchemaVersion,
		ActiveVersion:   transaction.TargetVersion,
		FallbackVersion: transaction.SourceVersion,
		State:           updateengine.StateTrial,
		TransactionID:   transaction.TransactionID,
		UpdatedAt:       time.Now().UTC(),
	}
	if err := driver.store.WriteActive(active); err != nil {
		return fmt.Errorf("activate target generation trial: %w", err)
	}
	if plan.CoreWasRunning {
		if err := driver.runStableCore(ctx, "service", "start", "--runtime-root", driver.root); err != nil {
			return fmt.Errorf("start target core: %w", err)
		}
	}
	return nil
}

func (driver *WindowsDriver) VerifyTrial(ctx context.Context, transaction updateengine.Transaction) ([]string, error) {
	plan, err := driver.plan(transaction)
	if err != nil {
		return nil, err
	}
	if plan.CoreWasRunning {
		if err := updateengine.WaitForVersion(ctx, plan.HealthURLs, transaction.TargetVersion, 45*time.Second); err != nil {
			return nil, fmt.Errorf("target core health/version check failed: %w", err)
		}
	}
	if plan.TrayWasRunning {
		if err := driver.startStableTray(ctx); err != nil {
			return nil, fmt.Errorf("start target tray: %w", err)
		}
		if err := driver.waitProcess(driver.layout.GenerationTray(transaction.TargetVersion), 15*time.Second); err != nil {
			return nil, fmt.Errorf("target tray did not stay running: %w", err)
		}
	}

	var warnings []string
	if plan.TunnelWasRunning {
		if err := driver.runStableCore(ctx, "tunnel", "start", "--runtime-root", driver.root); err != nil {
			warnings = append(warnings, "Tunnel could not be restored after update: "+err.Error())
		}
	}
	return warnings, nil
}

func (driver *WindowsDriver) Commit(_ context.Context, transaction updateengine.Transaction) error {
	// source generation 必须保留到 terminal result 落盘以后；这里仅把 pointer
	// 从 trial 收敛成 committed，不做任何不可逆清理。
	return driver.store.WriteActive(updateengine.ActiveVersion{
		SchemaVersion:   updateengine.SchemaVersion,
		ActiveVersion:   transaction.TargetVersion,
		FallbackVersion: transaction.SourceVersion,
		State:           updateengine.StateCommitted,
		UpdatedAt:       time.Now().UTC(),
	})
}

func (driver *WindowsDriver) Rollback(ctx context.Context, transaction updateengine.Transaction) error {
	plan, err := driver.plan(transaction)
	if err != nil {
		return err
	}
	var rollbackErrors []error

	// rollback 必须幂等。无论断电时 pointer 已经切到 target 还是仍在 source，
	// 先尽力停止两个 generation 的运行进程，再把唯一 active pointer 写回 source。
	_ = driver.runStableCore(ctx, "tunnel", "stop", "--runtime-root", driver.root)
	_ = driver.runStableCore(ctx, "service", "stop", "--runtime-root", driver.root)
	_ = desktopruntime.StopBinaryProcesses(ctx, driver.layout.GenerationTray(transaction.TargetVersion), 15*time.Second)

	if err := driver.store.WriteActive(updateengine.ActiveVersion{
		SchemaVersion:   updateengine.SchemaVersion,
		ActiveVersion:   transaction.SourceVersion,
		FallbackVersion: transaction.FallbackVersion,
		State:           updateengine.StateCommitted,
		UpdatedAt:       time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("restore source active pointer: %w", err)
	}

	if plan.CoreWasRunning {
		if err := driver.runStableCore(ctx, "service", "start", "--runtime-root", driver.root); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restart source core: %w", err))
		} else if err := updateengine.WaitForVersion(ctx, plan.HealthURLs, transaction.SourceVersion, 45*time.Second); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("source core health/version check failed: %w", err))
		}
	}
	if plan.TrayWasRunning {
		if err := driver.startStableTray(ctx); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restart source tray: %w", err))
		}
	}
	if plan.TunnelWasRunning {
		if err := driver.runStableCore(ctx, "tunnel", "start", "--runtime-root", driver.root); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restart source tunnel: %w", err))
		}
	}
	return errors.Join(rollbackErrors...)
}

func (driver *WindowsDriver) plan(transaction updateengine.Transaction) (*updateengine.WindowsPlan, error) {
	if transaction.Platform != "windows" || transaction.Windows == nil {
		return nil, errors.New("Windows arbiter requires a Windows transaction plan")
	}
	if !samePath(transaction.Windows.InstallRoot, driver.root) {
		return nil, errors.New("Windows transaction belongs to another install root")
	}
	return transaction.Windows, nil
}

func (driver *WindowsDriver) verifyGeneration(version, generationRoot string) error {
	if !samePath(generationRoot, driver.layout.GenerationDir(version)) {
		return fmt.Errorf("generation path %s does not match version %s", generationRoot, version)
	}
	for _, path := range []string{
		driver.layout.GenerationCore(version),
		driver.layout.GenerationTray(version),
		driver.layout.GenerationArbiter(version),
	} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("generation file is missing: %s", path)
		}
	}
	return nil
}

func (driver *WindowsDriver) runStableCore(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, driver.layout.CoreShim(), args...)
	command.Dir = driver.root
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, message)
	}
	return nil
}

func (driver *WindowsDriver) startStableTray(ctx context.Context) error {
	command := exec.CommandContext(ctx, driver.layout.TrayShim(), "--background")
	command.Dir = driver.root
	if output, err := command.CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, message)
	}
	return nil
}

func (driver *WindowsDriver) waitProcess(binaryPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := desktopruntime.BinaryProcessRunning(binaryPath)
		if err != nil {
			return err
		}
		if running {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("process is not running: %s", binaryPath)
}

func samePath(a, b string) bool {
	a, errA := filepath.Abs(a)
	b, errB := filepath.Abs(b)
	return errA == nil && errB == nil && strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
