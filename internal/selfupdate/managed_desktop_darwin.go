//go:build darwin

package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/fs/processlock"
	"github.com/uvwt/agentdock/internal/updateengine"
)

type macOSUpdateServiceState struct {
	SchemaVersion int  `json:"schema_version"`
	CoreEnabled   bool `json:"core_enabled"`
	TunnelEnabled bool `json:"tunnel_enabled"`
}

func applyManagedDesktopOnlyUpdate(ctx context.Context, request applyRequest) (applyResult, bool, error) {
	if err := validateDesktopUpdateCoordination(); err != nil {
		return applyResult{}, true, err
	}
	if strings.TrimSpace(request.DesktopTargetPath) == "" || strings.TrimSpace(request.DesktopStagedPath) == "" {
		return applyResult{}, true, errors.New("macOS App-managed update paths are incomplete")
	}

	sourceArbiter := filepath.Join(request.DesktopTargetPath, "Contents", "Helpers", "agentdock-arbiter")
	if !executableRegularFile(sourceArbiter) {
		// A pre-Arbiter App cannot manufacture a known-good source Arbiter after the fact.
		// Preserve the existing updater for exactly this bootstrap transition; the target App
		// embeds the Arbiter, so every following update uses the durable engine below.
		return applyResult{}, false, nil
	}

	root, err := macOSDesktopUpdateDirectory()
	if err != nil {
		return applyResult{}, true, err
	}
	store, err := updateengine.NewStore(root)
	if err != nil {
		return applyResult{}, true, err
	}
	stageLock, err := processlock.Acquire(ctx, filepath.Join(root, "update", "staging.lock"))
	if err != nil {
		return applyResult{}, true, err
	}
	defer stageLock.Release()
	transactionLock, acquired, err := processlock.TryAcquire(filepath.Join(root, "update", "transaction.lock"))
	if err != nil {
		return applyResult{}, true, err
	}
	if !acquired {
		return applyResult{}, true, errors.New("another AgentDock update transaction is already active")
	}
	if err := transactionLock.Release(); err != nil {
		return applyResult{}, true, err
	}

	transaction, err := updateengine.NewTransaction("darwin", request.CurrentVersion, request.TargetVersion)
	if err != nil {
		return applyResult{}, true, err
	}
	parent := filepath.Dir(filepath.Clean(request.DesktopTargetPath))
	trialPath := filepath.Join(parent, ".AgentDock.app.trial."+transaction.TransactionID)
	arbiterDir := filepath.Join(root, "update", "arbiters", transaction.TransactionID)
	arbiterPath := filepath.Join(arbiterDir, "agentdock-arbiter")
	cleanupPreflight := true
	defer func() {
		if cleanupPreflight {
			_ = os.RemoveAll(trialPath)
			_ = os.RemoveAll(arbiterDir)
		}
	}()

	if err := stageMacOSApp(ctx, request.DesktopStagedPath, trialPath, request.TargetVersion); err != nil {
		return applyResult{}, true, err
	}
	if err := copyKnownGoodMacOSArbiter(ctx, sourceArbiter, arbiterPath); err != nil {
		return applyResult{}, true, err
	}
	fmt.Fprintln(request.Output, "正在预检新版 AgentDock.app 与内置 Skill...")
	trialCore := filepath.Join(trialPath, "Contents", "Helpers", "agentdock")
	trialSkills := filepath.Join(trialPath, "Contents", "Resources", "core-skills")
	if err := bootstrapBundledSkills(ctx, trialCore, trialSkills, request.Output); err != nil {
		return applyResult{}, true, err
	}

	serviceStatePath := filepath.Join(root, "update-services.json")
	serviceState, err := readMacOSUpdateServiceState(serviceStatePath)
	if err != nil {
		return applyResult{}, true, err
	}
	appPIDs, err := runningMacOSAppPIDs(ctx, request.DesktopTargetPath)
	if err != nil {
		return applyResult{}, true, err
	}
	var healthURL string
	if serviceState.CoreEnabled {
		if candidates := platformHealthCandidates(ctx, request.CurrentPath); len(candidates) > 0 {
			healthURL = candidates[0]
		}
	}
	transaction.MacOS = &updateengine.MacOSPlan{
		SourceArbiterPath: arbiterPath,
		TargetAppPath:     filepath.Clean(request.DesktopTargetPath),
		TrialAppPath:      trialPath,
		HandoffPath:       filepath.Join(root, "update-handoff.json"),
		ResultPath:        filepath.Join(root, "update-result.json"),
		ServiceStatePath:  serviceStatePath,
		HealthURL:         healthURL,
		AppWasRunning:     len(appPIDs) > 0,
		CoreWasEnabled:    serviceState.CoreEnabled,
		TunnelEnabled:     serviceState.TunnelEnabled,
	}
	if err := store.WriteTransaction(transaction); err != nil {
		return applyResult{}, true, err
	}
	cleanupPreflight = false
	if len(appPIDs) > 0 {
		redirect, err := redirectProcessOutputForDesktopUpdate()
		if err != nil {
			return applyResult{}, true, fmt.Errorf("prepare independent macOS update log: %w", err)
		}
		// The GUI owns the original pipes and is intentionally terminated during atomic
		// activation. Keep fd 1/2 on update.log for the rest of this updater process.
		redirect.Commit()
	}

	reportUpdateStage(request.Progress, UpdateStageInstalling, request.CurrentVersion, request.TargetVersion, "")
	command := exec.CommandContext(
		ctx,
		arbiterPath,
		"--root", root,
		"--transaction-id", transaction.TransactionID,
	)
	command.Dir = root
	output, runErr := command.CombinedOutput()
	result, resultErr := store.ReadResult(transaction.TransactionID)
	if resultErr != nil {
		return applyResult{}, true, errors.Join(
			fmt.Errorf("macOS update Arbiter did not persist a terminal result: %w", resultErr),
			runErr,
		)
	}
	cleanupMacOSUpdateArtifactsForTerminalResult(
		result,
		trialPath,
		arbiterDir,
		transaction.MacOS.HandoffPath,
		transaction.MacOS.ServiceStatePath,
	)
	if result.State != updateengine.StateCommitted {
		message := terminalUpdateMessage(result)
		if strings.TrimSpace(string(output)) != "" {
			message += ": " + strings.TrimSpace(string(output))
		}
		if runErr != nil {
			return applyResult{}, true, fmt.Errorf("%s: %w", message, runErr)
		}
		return applyResult{}, true, errors.New(message)
	}

	fmt.Fprintf(request.Output, "macOS App 原子更新已提交：%s → %s\n", normalizeVersion(request.CurrentVersion), normalizeVersion(request.TargetVersion))
	return applyResult{}, true, nil
}

func stageMacOSApp(ctx context.Context, stagedPath, trialPath, targetVersion string) error {
	if err := os.RemoveAll(trialPath); err != nil {
		return err
	}
	parent := filepath.Dir(trialPath)
	probe, err := os.CreateTemp(parent, ".agentdock-update-permission-*")
	if err != nil {
		return fmt.Errorf("macOS App parent directory is not writable: %w", err)
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)
	output, err := exec.CommandContext(ctx, "/usr/bin/ditto", stagedPath, trialPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("stage macOS App trial: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := validateMacOSDesktopRuntime(ctx, trialPath, targetVersion); err != nil {
		return fmt.Errorf("validate staged macOS App trial: %w", err)
	}
	return nil
}

func copyKnownGoodMacOSArbiter(ctx context.Context, sourcePath, targetPath string) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return err
	}
	if output, err := exec.CommandContext(ctx, "/usr/bin/ditto", sourcePath, targetPath).CombinedOutput(); err != nil {
		return fmt.Errorf("copy known-good macOS Arbiter: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := os.Chmod(targetPath, 0o700); err != nil {
		return err
	}
	if output, err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--strict", "--verbose=2", targetPath).CombinedOutput(); err != nil {
		return fmt.Errorf("copied known-good macOS Arbiter signature is invalid: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func readMacOSUpdateServiceState(path string) (macOSUpdateServiceState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return macOSUpdateServiceState{}, fmt.Errorf("read macOS update service state: %w", err)
	}
	var state macOSUpdateServiceState
	if err := json.Unmarshal(data, &state); err != nil {
		return macOSUpdateServiceState{}, fmt.Errorf("parse macOS update service state: %w", err)
	}
	if state.SchemaVersion != 1 {
		return macOSUpdateServiceState{}, fmt.Errorf("unsupported macOS update service state schema: %d", state.SchemaVersion)
	}
	return state, nil
}

func cleanupMacOSUpdateArtifactsForTerminalResult(
	result updateengine.Result,
	trialPath, arbiterDir, handoffPath, serviceStatePath string,
) {
	// Only safe terminal outcomes release the rollback slot and copied known-good Arbiter.
	// A failed rollback must retain both plus the service journal for repair/recovery.
	if result.State != updateengine.StateCommitted && result.State != updateengine.StateRolledBack {
		return
	}
	cleanupMacOSUpdateArtifacts(trialPath, arbiterDir, handoffPath, serviceStatePath)
}

func cleanupMacOSUpdateArtifacts(trialPath, arbiterDir, handoffPath, serviceStatePath string) {
	_ = os.RemoveAll(trialPath)
	_ = os.RemoveAll(arbiterDir)
	for _, path := range []string{handoffPath, serviceStatePath} {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			// Cleanup is intentionally best effort after a safe terminal result is durable.
			// A later transaction uses unique rollback paths and rewrites coordination files.
			continue
		}
	}
}
