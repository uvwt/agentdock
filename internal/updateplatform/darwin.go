//go:build darwin

package updateplatform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
	"github.com/uvwt/agentdock/internal/updateengine"
)

type DarwinDriver struct {
	root  string
	store *updateengine.Store
}

type macOSHandoff struct {
	SchemaVersion      int    `json:"schema_version"`
	TransactionID      string `json:"transaction_id,omitempty"`
	TargetVersion      string `json:"target_version"`
	CoreRegistration   string `json:"core_registration,omitempty"`
	TunnelRegistration string `json:"tunnel_registration,omitempty"`
}

type macOSPendingResult struct {
	SchemaVersion  int    `json:"schema_version"`
	TransactionID  string `json:"transaction_id,omitempty"`
	OK             bool   `json:"ok"`
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	Message        string `json:"message"`
}

func NewDarwinDriver(root string) (*DarwinDriver, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("macOS update root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	store, err := updateengine.NewStore(absolute)
	if err != nil {
		return nil, err
	}
	return &DarwinDriver{root: absolute, store: store}, nil
}

func (driver *DarwinDriver) PrepareTrial(ctx context.Context, transaction updateengine.Transaction) error {
	plan, err := driver.plan(transaction)
	if err != nil {
		return err
	}
	if err := validateMacOSApp(ctx, plan.TargetAppPath, transaction.SourceVersion); err != nil {
		return fmt.Errorf("source App is not usable: %w", err)
	}
	if err := validateMacOSApp(ctx, plan.TrialAppPath, transaction.TargetVersion); err != nil {
		return fmt.Errorf("target App trial is not usable: %w", err)
	}
	if err := validateMacOSSigningContinuity(ctx, plan.TargetAppPath, plan.TrialAppPath); err != nil {
		return fmt.Errorf("target App signing identity does not match the source App: %w", err)
	}
	if err := removeIfExists(plan.HandoffPath); err != nil {
		return fmt.Errorf("remove stale macOS handoff: %w", err)
	}
	if err := terminateMacOSApp(ctx, plan.TargetAppPath, 15*time.Second); err != nil {
		return fmt.Errorf("stop source App: %w", err)
	}
	if err := writePendingResult(plan.ResultPath, macOSPendingResult{
		SchemaVersion:  1,
		TransactionID:  transaction.TransactionID,
		OK:             true,
		CurrentVersion: transaction.SourceVersion,
		TargetVersion:  transaction.TargetVersion,
		Message:        fmt.Sprintf("AgentDock is updating from %s to %s.", transaction.SourceVersion, transaction.TargetVersion),
	}); err != nil {
		return fmt.Errorf("write macOS trial result: %w", err)
	}
	if err := swapPathsAtomic(plan.TargetAppPath, plan.TrialAppPath); err != nil {
		return err
	}
	if err := validateMacOSApp(ctx, plan.TargetAppPath, transaction.TargetVersion); err != nil {
		return fmt.Errorf("active App after atomic swap is invalid: %w", err)
	}
	if err := launchMacOSApp(ctx, plan.TargetAppPath); err != nil {
		return err
	}
	return nil
}

func (driver *DarwinDriver) VerifyTrial(ctx context.Context, transaction updateengine.Transaction) ([]string, error) {
	plan, err := driver.plan(transaction)
	if err != nil {
		return nil, err
	}
	handoff, err := waitForMacOSHandoff(ctx, plan.HandoffPath, transaction.TransactionID, transaction.TargetVersion, 60*time.Second)
	if err != nil {
		return nil, err
	}
	var warnings []string
	if plan.CoreWasEnabled {
		switch handoff.CoreRegistration {
		case "requires_approval":
			warnings = append(warnings, "AgentDock Core requires background-item approval in System Settings.")
		case "enabled":
			if strings.TrimSpace(plan.HealthURL) == "" {
				return nil, errors.New("macOS trial requires a Core health URL when Core is enabled")
			}
			if err := updateengine.WaitForVersion(ctx, []string{plan.HealthURL}, transaction.TargetVersion, 45*time.Second); err != nil {
				return nil, fmt.Errorf("target Core health/version check failed: %w", err)
			}
		default:
			return nil, fmt.Errorf("target Core registration did not become usable: %s", handoff.CoreRegistration)
		}
	}
	if plan.TunnelEnabled {
		switch handoff.TunnelRegistration {
		case "requires_approval":
			warnings = append(warnings, "AgentDock Tunnel requires background-item approval in System Settings.")
		case "enabled":
			// Tunnel readiness depends on credentials/network/external Cloudflare state and is intentionally soft.
		default:
			warnings = append(warnings, "AgentDock Tunnel registration was not ready after update: "+handoff.TunnelRegistration)
		}
	}
	return warnings, nil
}

func (driver *DarwinDriver) Commit(context.Context, updateengine.Transaction) error {
	// The old App at TrialAppPath is the rollback slot. It must survive until the generic
	// Arbiter persists the terminal committed result; cleanup happens afterwards in selfupdate.
	return nil
}

func (driver *DarwinDriver) Rollback(ctx context.Context, transaction updateengine.Transaction) error {
	plan, err := driver.plan(transaction)
	if err != nil {
		return err
	}
	var rollbackErrors []error
	if err := terminateMacOSApp(ctx, plan.TargetAppPath, 15*time.Second); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("stop target App: %w", err))
	}

	activeVersion := macOSAppVersion(ctx, plan.TargetAppPath)
	trialVersion := macOSAppVersion(ctx, plan.TrialAppPath)
	sourceVersion := updateengine.NormalizeVersion(transaction.SourceVersion)
	targetVersion := updateengine.NormalizeVersion(transaction.TargetVersion)
	if err := restoreMacOSRollbackApp(
		plan.TargetAppPath,
		plan.TrialAppPath,
		activeVersion,
		trialVersion,
		sourceVersion,
		targetVersion,
	); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}

	if len(rollbackErrors) == 0 {
		if err := validateMacOSApp(ctx, plan.TargetAppPath, transaction.SourceVersion); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restored source App is invalid: %w", err))
		}
	}
	if err := removeIfExists(plan.HandoffPath); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("remove target handoff: %w", err))
	}
	if err := writePendingResult(plan.ResultPath, macOSPendingResult{
		SchemaVersion:  1,
		TransactionID:  transaction.TransactionID,
		OK:             false,
		CurrentVersion: transaction.SourceVersion,
		TargetVersion:  transaction.TargetVersion,
		Message:        "AgentDock update failed and the previous App was restored.",
	}); err != nil {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("write rollback result: %w", err))
	}
	shouldLaunchSource := plan.AppWasRunning || plan.CoreWasEnabled || plan.TunnelEnabled
	if shouldLaunchSource && len(rollbackErrors) == 0 {
		if err := launchMacOSApp(ctx, plan.TargetAppPath); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("restart source App: %w", err))
		}
	}
	if shouldLaunchSource && len(rollbackErrors) == 0 {
		handoff, err := waitForMacOSHandoff(
			ctx,
			plan.HandoffPath,
			transaction.TransactionID,
			transaction.SourceVersion,
			60*time.Second,
		)
		if err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("source App rollback handoff failed: %w", err))
		} else if plan.CoreWasEnabled {
			switch handoff.CoreRegistration {
			case "requires_approval":
				// User/system policy may prevent background execution. The source Bundle has still
				// been restored and rebound correctly, so this is not a rollback failure.
			case "enabled":
				if strings.TrimSpace(plan.HealthURL) == "" {
					rollbackErrors = append(rollbackErrors, errors.New("macOS rollback requires a Core health URL when Core was enabled"))
				} else if err := updateengine.WaitForVersion(ctx, []string{plan.HealthURL}, transaction.SourceVersion, 45*time.Second); err != nil {
					rollbackErrors = append(rollbackErrors, fmt.Errorf("restored source Core health/version check failed: %w", err))
				}
			default:
				rollbackErrors = append(rollbackErrors, fmt.Errorf("source Core registration did not become usable after rollback: %s", handoff.CoreRegistration))
			}
		}
	}
	return errors.Join(rollbackErrors...)
}

func restoreMacOSRollbackApp(targetPath, trialPath, activeVersion, trialVersion, sourceVersion, targetVersion string) error {
	switch {
	case activeVersion == targetVersion && trialVersion == sourceVersion:
		if err := swapPathsAtomic(targetPath, trialPath); err != nil {
			return fmt.Errorf("atomic rollback App swap: %w", err)
		}
	case activeVersion == sourceVersion:
		// PrepareTrial failed before the swap, or a previous recovery already restored source.
		return nil
	case activeVersion == "" && trialVersion == sourceVersion:
		// active App may disappear if the machine crashes or external cleanup happens after the
		// source Bundle has entered the rollback slot. Because both paths are siblings and the
		// slot still proves the exact source version, an atomic rename safely restores source.
		if err := os.Rename(trialPath, targetPath); err != nil {
			return fmt.Errorf("restore missing active App from rollback slot: %w", err)
		}
	default:
		return fmt.Errorf(
			"cannot prove safe App rollback state: active=%s trial=%s source=%s target=%s",
			activeVersion, trialVersion, sourceVersion, targetVersion,
		)
	}
	return nil
}

func (driver *DarwinDriver) plan(transaction updateengine.Transaction) (*updateengine.MacOSPlan, error) {
	if transaction.Platform != "darwin" || transaction.MacOS == nil {
		return nil, errors.New("macOS Arbiter requires a macOS transaction plan")
	}
	plan := transaction.MacOS
	if strings.TrimSpace(plan.TargetAppPath) == "" || strings.TrimSpace(plan.TrialAppPath) == "" {
		return nil, errors.New("macOS transaction App paths are required")
	}
	if filepath.Dir(filepath.Clean(plan.TargetAppPath)) != filepath.Dir(filepath.Clean(plan.TrialAppPath)) {
		return nil, errors.New("macOS target and trial App must be siblings")
	}
	if strings.TrimSpace(plan.HandoffPath) == "" || strings.TrimSpace(plan.ResultPath) == "" {
		return nil, errors.New("macOS transaction coordination paths are required")
	}
	return plan, nil
}

func validateMacOSApp(ctx context.Context, appPath, expectedVersion string) error {
	info, err := os.Lstat(appPath)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("App Bundle is unavailable: %s", appPath)
	}
	identifier, err := plistValue(ctx, filepath.Join(appPath, "Contents", "Info.plist"), "CFBundleIdentifier")
	if err != nil || identifier != "com.uvwt.agentdock" {
		return fmt.Errorf("unexpected Bundle Identifier: %q", identifier)
	}
	version := macOSAppVersion(ctx, appPath)
	if version != updateengine.NormalizeVersion(expectedVersion) {
		return fmt.Errorf("App version is %s, want %s", version, updateengine.NormalizeVersion(expectedVersion))
	}
	core := filepath.Join(appPath, "Contents", "Helpers", "agentdock")
	arbiter := filepath.Join(appPath, "Contents", "Helpers", "agentdock-arbiter")
	for _, path := range []string{core, arbiter} {
		fileInfo, statErr := os.Lstat(path)
		if statErr != nil || !fileInfo.Mode().IsRegular() || fileInfo.Mode()&0o111 == 0 {
			return fmt.Errorf("required App helper is unavailable: %s", path)
		}
	}
	if output, err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", "--verbose=2", appPath).CombinedOutput(); err != nil {
		return fmt.Errorf("App signature verification failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if output, err := exec.CommandContext(ctx, core, "version", "--json").CombinedOutput(); err != nil {
		return fmt.Errorf("read App Core version: %w: %s", err, strings.TrimSpace(string(output)))
	} else {
		var build struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(output, &build) != nil || updateengine.NormalizeVersion(build.Version) != updateengine.NormalizeVersion(expectedVersion) {
			return fmt.Errorf("App Core version is %s, want %s", build.Version, updateengine.NormalizeVersion(expectedVersion))
		}
	}
	return nil
}

func validateMacOSSigningContinuity(ctx context.Context, sourceAppPath, targetAppPath string) error {
	requirement, err := macOSDesignatedRequirement(ctx, sourceAppPath)
	if err != nil {
		return fmt.Errorf("read source App designated requirement: %w", err)
	}
	if isLegacyAdHocRequirement(requirement) {
		// Existing 0.8.x desktop builds were ad-hoc signed, whose designated requirement is
		// a per-build cdhash. Such a requirement can never match a different version. Allow
		// this one compatibility boundary; once a certificate-signed App is active, every
		// following update must satisfy the source certificate requirement below.
		return nil
	}
	output, err := exec.CommandContext(
		ctx,
		"/usr/bin/codesign",
		"--verify",
		"--strict",
		"-R="+requirement,
		targetAppPath,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("target App does not satisfy source signing requirement: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func macOSDesignatedRequirement(ctx context.Context, appPath string) (string, error) {
	output, err := exec.CommandContext(ctx, "/usr/bin/codesign", "-d", "-r-", appPath).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("codesign designated requirement: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return parseMacOSDesignatedRequirement(string(output))
}

func parseMacOSDesignatedRequirement(output string) (string, error) {
	const marker = "designated =>"
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if index := strings.Index(line, marker); index >= 0 {
			requirement := strings.TrimSpace(line[index+len(marker):])
			if requirement != "" {
				return requirement, nil
			}
		}
	}
	return "", errors.New("codesign did not report a designated requirement")
}

func isLegacyAdHocRequirement(requirement string) bool {
	normalized := strings.ToLower(strings.TrimSpace(requirement))
	return strings.Contains(normalized, "cdhash") && !strings.Contains(normalized, "certificate")
}

func macOSAppVersion(ctx context.Context, appPath string) string {
	version, err := plistValue(ctx, filepath.Join(appPath, "Contents", "Info.plist"), "CFBundleShortVersionString")
	if err != nil {
		return ""
	}
	return updateengine.NormalizeVersion(version)
}

func plistValue(ctx context.Context, plistPath, key string) (string, error) {
	output, err := exec.CommandContext(ctx, "/usr/bin/plutil", "-extract", key, "raw", "-o", "-", plistPath).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("plutil %s: %w: %s", key, err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func writePendingResult(path string, result macOSPendingResult) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("macOS pending result path is required")
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicfile.Write(path, data, 0o600)
}

func waitForMacOSHandoff(ctx context.Context, path, transactionID, targetVersion string, timeout time.Duration) (macOSHandoff, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			var handoff macOSHandoff
			if err := json.Unmarshal(data, &handoff); err != nil {
				return macOSHandoff{}, fmt.Errorf("parse macOS handoff: %w", err)
			}
			if handoff.SchemaVersion != 1 || handoff.TransactionID != transactionID {
				return macOSHandoff{}, errors.New("macOS handoff belongs to another update transaction")
			}
			if updateengine.NormalizeVersion(handoff.TargetVersion) != updateengine.NormalizeVersion(targetVersion) {
				return macOSHandoff{}, fmt.Errorf("macOS handoff version %s does not match target %s", handoff.TargetVersion, targetVersion)
			}
			return handoff, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return macOSHandoff{}, err
		}
		select {
		case <-ctx.Done():
			return macOSHandoff{}, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return macOSHandoff{}, errors.New("new AgentDock.app did not acknowledge the update transaction before timeout")
}

func launchMacOSApp(ctx context.Context, appPath string) error {
	command := exec.CommandContext(ctx, "/usr/bin/open", macOSOpenArguments(appPath)...)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("launch AgentDock.app: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func macOSOpenArguments(appPath string) []string {
	args := []string{"-g", "-n"}
	// `open` launches GUI apps through LaunchServices, which does not guarantee that shell
	// environment overrides are copied into the new process. Preserve the standard home
	// overrides used by isolated/runtime-managed environments explicitly so the new App and
	// its SMAppService registrations keep the same state root as the updater.
	for _, name := range []string{"HOME", "CFFIXED_USER_HOME", "AGENTDOCK_SKIP_LOGIN_ITEM_CONFIGURATION"} {
		if value, ok := os.LookupEnv(name); ok && strings.TrimSpace(value) != "" {
			args = append(args, "--env", name+"="+value)
		}
	}
	args = append(args, appPath, "--args", "--background")
	return args
}

func terminateMacOSApp(ctx context.Context, appPath string, timeout time.Duration) error {
	executable := filepath.Join(filepath.Clean(appPath), "Contents", "MacOS", "AgentDock")
	pids, err := processIDsAtExecutable(ctx, executable)
	if err != nil {
		return err
	}
	for _, pid := range pids {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remaining, err := processIDsAtExecutable(ctx, executable)
		if err != nil {
			return err
		}
		if len(remaining) == 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("AgentDock.app did not exit within %s", timeout)
}

func processIDsAtExecutable(ctx context.Context, executable string) ([]int, error) {
	output, err := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return nil, err
	}
	return processIDsFromPSOutput(output, executable), nil
}

func processIDsFromPSOutput(output []byte, executable string) []int {
	cleanExecutable := filepath.Clean(executable)
	var pids []int
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		separator := strings.IndexByte(line, ' ')
		if separator <= 0 {
			continue
		}
		pidText := strings.TrimSpace(line[:separator])
		command := strings.TrimSpace(line[separator+1:])
		// command= 保留完整 argv；不能用 strings.Fields 拆路径，否则带空格的 App 路径
		// 会被截断。只接受精确 executable 或 executable 后跟参数，避免前缀误匹配。
		if command != cleanExecutable && !strings.HasPrefix(command, cleanExecutable+" ") {
			continue
		}
		pid, err := strconv.Atoi(pidText)
		if err == nil && pid != os.Getpid() {
			pids = append(pids, pid)
		}
	}
	return pids
}

func removeIfExists(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
