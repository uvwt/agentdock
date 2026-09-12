package installer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/fs/processlock"
	skills "github.com/uvwt/agentdock/internal/skill"
	skillbundle "github.com/uvwt/agentdock/internal/skill/bundle"
	skillstate "github.com/uvwt/agentdock/internal/skill/state"
	"github.com/uvwt/agentdock/internal/updateengine"
)

type Engine struct{}

func (engine Engine) Run(ctx context.Context, request Request) (Result, error) {
	request, err := normalizeRequest(request)
	if err != nil {
		return Result{}, err
	}
	installRoot, err := absPath(request.InstallRoot)
	if err != nil {
		return Result{}, fmt.Errorf("install-root: %w", err)
	}
	runtimeRoot, err := absPath(request.RuntimeRoot)
	if err != nil {
		return Result{}, fmt.Errorf("runtime-root: %w", err)
	}
	if request.RuntimeRootLiteral == "" {
		request.RuntimeRootLiteral = request.RuntimeRoot
	}
	request.InstallRoot = installRoot
	request.RuntimeRoot = runtimeRoot

	store, err := NewStore(request.StateRoot())
	if err != nil {
		return Result{}, err
	}
	lock, err := processlock.Acquire(ctx, store.LockPath())
	if err != nil {
		return Result{}, fmt.Errorf("lock install transaction: %w", err)
	}
	defer lock.Release()

	switch request.Action {
	case ActionUninstall:
		if recovered, err := engine.recoverInterrupted(ctx, store, request); err != nil {
			return recovered, err
		}
		return engine.uninstall(ctx, store, request)
	case ActionAbandon:
		return engine.abandon(store, request)
	case ActionCommit:
		return engine.commit(store, request)
	case ActionRepair:
		request.Channel = "repair"
		if recovered, err := engine.recoverInterrupted(ctx, store, request); err != nil {
			return recovered, err
		}
		return engine.install(ctx, store, request)
	default:
		if recovered, err := engine.recoverInterrupted(ctx, store, request); err != nil {
			return recovered, err
		}
		return engine.install(ctx, store, request)
	}
}

const installRecoveryTimeout = 3 * time.Minute

func (engine Engine) recoverInterrupted(ctx context.Context, store *Store, request Request) (Result, error) {
	transaction, err := store.ReadTransaction()
	if err != nil {
		if os.IsNotExist(err) {
			return Result{}, nil
		}
		return Result{}, err
	}
	journal, err := loadJournal(store.Root(), transaction.TransactionID)
	if err != nil {
		return Result{}, fmt.Errorf("load interrupted install journal: %w", err)
	}
	switch transaction.State {
	case updateengine.StateCommitted, updateengine.StateRolledBack:
		return Result{}, nil
	case updateengine.StateFailed:
		// rollback_failed 必须重试 Restore。没有 journal 且已经动过文件时，
		// 每次 install 都要拒绝，不能第二次当成“已处理完”把 v2 文件当 v1 source。
		if journal == nil {
			if installPhaseMayHaveMutatedFiles(transaction.Phase) {
				return resultFromTransaction(transaction), errors.New("interrupted install has no rollback journal after files may have changed")
			}
			return Result{}, nil
		}
	case "":
		return Result{}, nil
	}

	// 未终结的 install 不能当下一次 known-good source。先按 journal 恢复文件/服务，
	// 再把权威状态写成 rolled_back 或 rollback_failed。
	result := resultFromTransaction(transaction)
	result.Healthy = false
	result.ActiveVersion = transaction.SourceVersion
	result.FallbackVersion = transaction.SourceVersion
	result.Failure = &updateengine.Failure{
		Code:    "trial-interrupted",
		Message: "previous install trial was interrupted before commit",
		At:      time.Now().UTC(),
	}
	if journal == nil {
		// 没有 journal 就不能宣称文件已回到安装前。
		completed, completeErr := store.Complete(transaction, updateengine.StateFailed, result)
		if completeErr != nil {
			return result, completeErr
		}
		if !installPhaseMayHaveMutatedFiles(transaction.Phase) {
			return completed, nil
		}
		return completed, errors.New("interrupted install has no rollback journal after files may have changed")
	}
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), installRecoveryTimeout)
	defer cancel()
	if rollbackErr := journal.Restore(rollbackCtx, request); rollbackErr != nil {
		result.Failure.Code = "rollback_failed"
		result.Failure.Message = errors.Join(errors.New(result.Failure.Message), rollbackErr).Error()
		transaction.ActiveVersion = transaction.SourceVersion
		transaction.FallbackVersion = transaction.SourceVersion
		completed, completeErr := store.Complete(transaction, updateengine.StateFailed, result)
		if completeErr != nil {
			return result, errors.Join(rollbackErr, completeErr)
		}
		return completed, rollbackErr
	}
	result.ActiveVersion = transaction.SourceVersion
	result.FallbackVersion = transaction.SourceVersion
	transaction.ActiveVersion = transaction.SourceVersion
	transaction.FallbackVersion = transaction.SourceVersion
	completed, err := store.Complete(transaction, updateengine.StateRolledBack, result)
	if err != nil {
		return result, err
	}
	return completed, nil
}

func (engine Engine) install(ctx context.Context, store *Store, request Request) (Result, error) {
	platform := currentPlatform()
	sourceVersion := existingVersion(request)
	if request.Version == "" {
		// repair 没有新 payload 时，目标就是当前还在跑的版本，不能改用当前进程的 buildinfo。
		if request.Action == ActionRepair && request.PayloadDir == "" && sourceVersion != "" {
			request.Version = sourceVersion
		} else if version, err := payloadVersion(request); err == nil {
			request.Version = version
		} else {
			request.Version = "unknown"
		}
	}

	transaction, err := newTransaction(request, platform, sourceVersion)
	if err != nil {
		return Result{}, err
	}
	if err := store.WriteTransaction(transaction); err != nil {
		return Result{}, err
	}
	journal := newJournal(store.Root(), transaction.TransactionID)
	result := Result{
		SchemaVersion: SchemaVersion,
		TransactionID: transaction.TransactionID,
		Platform:      platform,
		Action:        request.Action,
		Version:       request.Version,
		StartedAt:     transaction.StartedAt,
	}

	fail := func(phase Phase, err error, staged stagedInstall) (Result, error) {
		transaction.Phase = PhaseRollback
		result.Healthy = false
		result.Failure = &updateengine.Failure{Code: string(phase) + "_failed", Message: err.Error(), At: time.Now().UTC()}
		if staged.Journal != nil {
			if rollbackErr := rollbackInstall(ctx, request, staged); rollbackErr != nil {
				result.Failure.Code = "rollback_failed"
				result.Failure.Message = errors.Join(err, rollbackErr).Error()
				result.ActiveVersion = transaction.SourceVersion
				result.FallbackVersion = transaction.SourceVersion
				transaction.ActiveVersion = transaction.SourceVersion
				transaction.FallbackVersion = transaction.SourceVersion
				completed, completeErr := store.Complete(transaction, updateengine.StateFailed, result)
				if completeErr != nil {
					return result, errors.Join(err, rollbackErr, completeErr)
				}
				return completed, errors.Join(err, rollbackErr)
			}
			// 文件和服务已经回到安装前。Version 仍记录失败目标，ActiveVersion 必须是真正在跑的源版本。
			result.ActiveVersion = transaction.SourceVersion
			transaction.ActiveVersion = transaction.SourceVersion
			transaction.FallbackVersion = transaction.SourceVersion
			result.FallbackVersion = transaction.SourceVersion
			completed, completeErr := store.Complete(transaction, updateengine.StateRolledBack, result)
			if completeErr != nil {
				return result, errors.Join(err, completeErr)
			}
			return completed, err
		}
		transaction.Phase = phase
		completed, completeErr := store.Complete(transaction, updateengine.StateFailed, result)
		if completeErr != nil {
			return result, errors.Join(err, completeErr)
		}
		return completed, err
	}

	transaction.Phase = PhaseVerify
	if err := store.WriteTransaction(transaction); err != nil {
		return fail(PhaseVerify, err, stagedInstall{})
	}
	if request.Action == ActionRepair && request.PayloadDir == "" && request.BinaryPath == "" {
		request.BinaryPath = repairBinary(request)
	}
	if err := verifyRequest(request); err != nil {
		return fail(PhaseVerify, err, stagedInstall{})
	}

	transaction.Phase = PhaseStage
	if err := store.WriteTransaction(transaction); err != nil {
		return fail(PhaseStage, err, stagedInstall{})
	}
	staged, err := stagePayload(request, journal)
	if err != nil {
		return fail(PhaseStage, err, stagedInstall{Journal: journal})
	}

	transaction.Phase = PhaseActivate
	transaction.State = updateengine.StateTrial
	if err := store.WriteTransaction(transaction); err != nil {
		return fail(PhaseActivate, err, staged)
	}
	activated, err := activateInstall(ctx, request, staged)
	if err != nil {
		return fail(PhaseActivate, err, staged)
	}
	result.LocalMCPURL = activated.LocalMCPURL
	result.PublicURL = activated.PublicURL
	result.PrivilegeMode = activated.PrivilegeMode
	result.ActiveVersion = activated.ActiveVersion
	result.Warnings = append(result.Warnings, activated.Warnings...)
	transaction.ActiveVersion = activated.ActiveVersion

	if request.StartService {
		transaction.Phase = PhaseStart
		if err := store.WriteTransaction(transaction); err != nil {
			return fail(PhaseStart, err, staged)
		}
		if err := startPlatformServices(ctx, request, staged.Journal); err != nil {
			return fail(PhaseStart, err, staged)
		}

		if !request.SkipHealth && shouldWaitForHealth(request) {
			transaction.Phase = PhaseHealth
			if err := store.WriteTransaction(transaction); err != nil {
				return fail(PhaseHealth, err, staged)
			}
			host, port := resolveListenAddress(request)
			endpoint := healthURL(host, port)
			var healthErr error
			if runtimeGOOS() != "darwin" && request.Version != "unknown" {
				healthErr = updateengine.WaitForVersion(ctx, []string{endpoint}, strings.TrimPrefix(request.Version, "v"), 45*time.Second)
			}
			if waitErr := waitHealthyWithProbe(ctx, request, endpoint, 45*time.Second); waitErr != nil {
				if healthErr != nil {
					waitErr = errors.Join(healthErr, waitErr)
				}
				return fail(PhaseHealth, waitErr, staged)
			}
			result.Healthy = true
		}
	}

	if !request.SkipSkills && strings.TrimSpace(staged.SkillBundle) != "" {
		transaction.Phase = PhaseSkills
		if err := store.WriteTransaction(transaction); err != nil {
			return fail(PhaseSkills, err, staged)
		}
		if err := bootstrapSkills(ctx, request, staged.SkillBundle); err != nil {
			return fail(PhaseSkills, err, staged)
		}
	}

	if request.StartService && (request.TunnelMode == "quick" || request.TunnelMode == "named") {
		transaction.Phase = PhaseTunnel
		if err := store.WriteTransaction(transaction); err != nil {
			return fail(PhaseTunnel, err, staged)
		}
		if err := startTunnelServices(ctx, request, staged.Journal); err != nil {
			return fail(PhaseTunnel, err, staged)
		}
		if err := waitTunnelReady(ctx, request, 45*time.Second); err != nil {
			return fail(PhaseTunnel, err, staged)
		}
		if request.TunnelMode == "quick" {
			if publicURL := readQuickTunnelURL(request.RuntimeRoot); publicURL != "" {
				result.PublicURL = publicURL
			}
		}
	}

	if request.DeferCommit {
		// 平台外部状态还没被 OS adapter 确认。现在只能停在 trial，
		// committed 必须由后续 install commit 单独写入。
		transaction.State = updateengine.StateTrial
		transaction.Phase = PhaseCommit
		if err := store.WriteTransaction(transaction); err != nil {
			return result, err
		}
		result.SchemaVersion = SchemaVersion
		result.TransactionID = transaction.TransactionID
		result.Platform = platform
		result.Action = request.Action
		result.State = updateengine.StateTrial
		result.Phase = PhaseCommit
		result.StartedAt = transaction.StartedAt
		if err := store.WriteResult(result); err != nil {
			return result, err
		}
		return result, nil
	}

	transaction.Phase = PhaseCommit
	completed, err := store.Complete(transaction, updateengine.StateCommitted, result)
	if err != nil {
		return result, err
	}
	return completed, nil
}

func installPhaseMayHaveMutatedFiles(phase Phase) bool {
	switch phase {
	case PhasePrepare, PhaseVerify, "":
		return false
	default:
		return true
	}
}

func bindInstallTransaction(store *Store, request Request) (Transaction, Result, error) {
	transaction, err := store.ReadTransaction()
	if err != nil {
		return Transaction{}, Result{}, err
	}
	want := strings.TrimSpace(request.TransactionID)
	if want != "" && transaction.TransactionID != want {
		return Transaction{}, Result{}, fmt.Errorf("install 事务不匹配：journal=%s, 请求=%s", transaction.TransactionID, want)
	}
	current, err := store.ReadResult(transaction.TransactionID)
	if err != nil {
		current = resultFromTransaction(transaction)
	}
	if current.TransactionID != "" && current.TransactionID != transaction.TransactionID {
		current = resultFromTransaction(transaction)
	}
	return transaction, current, nil
}

func (engine Engine) commit(store *Store, request Request) (Result, error) {
	transaction, current, err := bindInstallTransaction(store, request)
	if err != nil {
		return Result{}, fmt.Errorf("commit: %w", err)
	}
	if transaction.State == updateengine.StateCommitted && current.TransactionID == transaction.TransactionID {
		return current, nil
	}
	if transaction.State != updateengine.StateTrial {
		return current, fmt.Errorf("install commit 只能结束 trial，当前 state=%s transaction=%s", transaction.State, transaction.TransactionID)
	}
	return store.Complete(transaction, updateengine.StateCommitted, current)
}

func (engine Engine) abandon(store *Store, request Request) (Result, error) {
	transaction, current, err := bindInstallTransaction(store, request)
	if err != nil {
		return Result{}, fmt.Errorf("abandon: %w", err)
	}

	seal := func(state updateengine.State, code, message string) (Result, error) {
		current.ActiveVersion = transaction.SourceVersion
		current.FallbackVersion = transaction.SourceVersion
		current.Healthy = false
		transaction.Phase = PhaseRollback
		transaction.ActiveVersion = transaction.SourceVersion
		transaction.FallbackVersion = transaction.SourceVersion
		current.Failure = &updateengine.Failure{Code: code, Message: message, At: time.Now().UTC()}
		return store.Complete(transaction, state, current)
	}

	if request.RollbackFailed {
		return seal(updateengine.StateFailed, "rollback_failed", "OS adapter 回滚外部状态失败，权威事务不能写成 rolled_back")
	}
	if current.TransactionID == transaction.TransactionID && current.State == updateengine.StateRolledBack && current.Phase == PhaseRollback {
		return current, nil
	}
	if current.TransactionID == transaction.TransactionID && current.State == updateengine.StateFailed {
		return current, nil
	}
	switch transaction.State {
	case updateengine.StateCommitted, updateengine.StateTrial, updateengine.StateStaged, updateengine.StateRollingBack, updateengine.StateRolledBack:
	default:
		return current, fmt.Errorf("install abandon 不能处理 state=%s", transaction.State)
	}
	return seal(updateengine.StateRolledBack, "abandoned", "OS adapter 已完成外部回滚，权威状态撤销为 rolled_back")
}

func (engine Engine) uninstall(ctx context.Context, store *Store, request Request) (Result, error) {
	platform := currentPlatform()
	transaction, err := newTransaction(request, platform, existingVersion(request))
	if err != nil {
		return Result{}, err
	}
	transaction.Phase = PhaseRollback
	if err := store.WriteTransaction(transaction); err != nil {
		return Result{}, err
	}
	if err := uninstallPlatform(ctx, request); err != nil {
		result := Result{
			Failure: &updateengine.Failure{Code: "uninstall_failed", Message: err.Error(), At: time.Now().UTC()},
			Version: request.Version,
		}
		completed, completeErr := store.Complete(transaction, updateengine.StateFailed, result)
		if completeErr != nil {
			return result, errors.Join(err, completeErr)
		}
		return completed, err
	}
	result := Result{Version: request.Version, Healthy: false}
	completed, err := store.Complete(transaction, updateengine.StateCommitted, result)
	if err != nil {
		return result, err
	}
	if request.PurgeConfig {
		for _, name := range []string{"agentdock.env", "cloudflared.env", "desktop-runtime.json", "runtime.json", "active-version.json"} {
			_ = os.Remove(filepath.Join(request.RuntimeRoot, name))
		}
	}
	if request.PurgeData {
		_ = os.RemoveAll(request.InstallRoot)
		if request.RuntimeRoot != request.InstallRoot {
			_ = os.RemoveAll(request.RuntimeRoot)
		}
	}
	return completed, nil
}

func runtimeGOOS() string {
	return currentPlatform()
}

func bootstrapSkills(ctx context.Context, request Request, bundleDir string) error {
	home := strings.TrimSpace(request.AgentDockHome)
	if home == "" && request.DataDir != "" {
		home = filepath.Join(request.DataDir, ".agentdock")
	}
	if home == "" {
		return nil
	}
	if err := os.Setenv("AGENTDOCK_HOME", home); err != nil {
		return err
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if err := cfg.Normalize(); err != nil {
		return err
	}
	stateDir, err := config.SkillStateDir(cfg)
	if err != nil {
		return err
	}
	state, err := skillstate.New(stateDir)
	if err != nil {
		return err
	}
	manager, err := skills.New(state)
	if err != nil {
		return err
	}
	_, err = skillbundle.Bootstrap(ctx, state, manager, bundleDir)
	return err
}

func verifyRequest(request Request) error {
	if request.Action == ActionUninstall {
		return nil
	}
	if request.PayloadDir == "" && request.BinaryPath == "" {
		if request.Action == ActionRepair {
			return errors.New("repair 找不到已安装的 binary；请提供 --payload-dir 或 --binary")
		}
		return errors.New("install 需要 --payload-dir 或 --binary")
	}
	if request.PayloadDir != "" {
		info, err := os.Stat(request.PayloadDir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("payload-dir 无效：%s", request.PayloadDir)
		}
	}
	if request.BinaryPath != "" {
		info, err := os.Stat(request.BinaryPath)
		if err != nil || info.IsDir() {
			return fmt.Errorf("binary 无效：%s", request.BinaryPath)
		}
	}
	if request.TunnelMode == "named" && strings.TrimSpace(request.ServerURL) == "" {
		return errors.New("Named Tunnel 必须提供 --server-url")
	}
	return nil
}

func existingVersion(request Request) string {
	store, err := NewStore(request.StateRoot())
	if err != nil {
		return windowsCommittedGeneration(request)
	}
	transaction, err := store.ReadTransaction()
	if err == nil {
		switch transaction.State {
		case updateengine.StateCommitted:
			if version := strings.TrimSpace(transaction.ActiveVersion); version != "" {
				return version
			}
			return strings.TrimSpace(transaction.TargetVersion)
		case updateengine.StateRolledBack, updateengine.StateFailed, updateengine.StateTrial, updateengine.StateStaged, updateengine.StateRollingBack:
			// trial / rollback_failed 的 ActiveVersion 可能仍是失败目标，只能回到 source。
			return strings.TrimSpace(transaction.SourceVersion)
		}
	}
	current, err := store.ReadCurrentResult()
	if err == nil && current.State == updateengine.StateCommitted {
		if version := strings.TrimSpace(current.ActiveVersion); version != "" {
			return version
		}
		return strings.TrimSpace(current.Version)
	}
	return windowsCommittedGeneration(request)
}

func windowsCommittedGeneration(request Request) string {
	store, err := updateengine.NewStore(request.InstallRoot)
	if err != nil {
		return ""
	}
	active, err := store.ReadActive()
	if err != nil || active.State != updateengine.StateCommitted {
		return ""
	}
	return strings.TrimSpace(active.ActiveVersion)
}

func repairBinary(request Request) string {
	if runtimeGOOS() == "windows" {
		if layout, err := updateengine.NewWindowsLayout(request.InstallRoot); err == nil {
			if fileExists(layout.CoreShim()) {
				return layout.CoreShim()
			}
			if version := existingVersion(request); version != "" && fileExists(layout.GenerationCore(version)) {
				return layout.GenerationCore(version)
			}
		}
		for _, candidate := range []string{
			filepath.Join(request.InstallRoot, "bin", "agentdock.exe"),
			filepath.Join(request.InstallRoot, "agentdock.exe"),
		} {
			if fileExists(candidate) {
				return candidate
			}
		}
		return ""
	}
	live := unixLiveBinary(request)
	if fileExists(live) {
		return live
	}
	nested := filepath.Join(request.InstallRoot, "bin", "agentdock")
	if fileExists(nested) {
		return nested
	}
	direct := filepath.Join(request.InstallRoot, "agentdock")
	if fileExists(direct) {
		return direct
	}
	return ""
}

func randomHex(byteCount int) (string, error) {
	raw := make([]byte, byteCount)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func copyTree(src, dst string, mode os.FileMode) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTree(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()), mode); err != nil {
				return err
			}
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if mode == 0 {
		mode = info.Mode()
	}
	return os.WriteFile(dst, data, mode)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
