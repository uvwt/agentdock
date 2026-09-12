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

	// 卸载事务没有 install rollback journal，不能按安装中断去 Restore。
	if transaction.Action == ActionUninstall {
		return Result{}, nil
	}

	// 终态事务以 transaction.json 为准，不能先读 rollback journal。
	// 成功安装后旧 journal 损坏或被清掉，都不能阻断下一次 install。
	switch transaction.State {
	case updateengine.StateCommitted:
		if err := commitWindowsActivePointer(transaction.InstallRoot, transaction.TransactionID); err != nil {
			return resultFromTransaction(transaction), err
		}
		discardJournal(store.Root(), transaction.TransactionID)
		return Result{}, nil
	case updateengine.StateRolledBack:
		if err := releaseWindowsTrialPointer(transaction.InstallRoot, transaction.TransactionID); err != nil {
			return resultFromTransaction(transaction), fmt.Errorf("previous rollback left a trial generation pointer: %w", err)
		}
		discardJournal(store.Root(), transaction.TransactionID)
		return Result{}, nil
	case "":
		return Result{}, nil
	case updateengine.StateFailed:
		if isExternalRollbackFailure(transaction) {
			if request.Action == ActionUninstall {
				return Result{}, nil
			}
			result := projectRestoredResult(resultFromTransaction(transaction), transaction, request, false)
			return result, errors.New("previous OS adapter rollback failed; repair Task/Registry/service state, then run install abandon without --rollback-failed")
		}
	}

	// pointer 已随本事务 committed、权威事务还停在 trial：只把事务补写成 committed。
	// 绝不能按中断 trial 回滚，否则会删掉 committed pointer 仍指向的 generation。
	if transaction.State == updateengine.StateTrial {
		owned, err := windowsPointerCommittedBy(transaction.InstallRoot, transaction.TransactionID)
		if err != nil {
			return resultFromTransaction(transaction), err
		}
		if owned {
			current, readErr := store.ReadResult(transaction.TransactionID)
			if readErr != nil {
				current = resultFromTransaction(transaction)
			}
			if _, err := commitPreparedInstall(store, transaction, current); err != nil {
				return current, err
			}
			return Result{}, nil
		}
	}

	journal, err := loadJournal(store.Root(), transaction.TransactionID)
	if err != nil {
		return Result{}, fmt.Errorf("load interrupted install journal: %w", err)
	}

	if transaction.State == updateengine.StateFailed && journal == nil {
		// Engine-owned rollback_failed 必须重试 Restore。没有 journal 且已经动过文件时，
		// 每次 install 都要拒绝，不能第二次当成“已处理完”把 v2 文件当 v1 source。
		if installPhaseMayHaveMutatedFiles(transaction.Phase) {
			return resultFromTransaction(transaction), errors.New("interrupted install has no rollback journal after files may have changed")
		}
		return Result{}, nil
	}

	// 未终结的 install 不能当下一次 known-good source。先按 journal 恢复文件/服务，
	// 再把权威状态写成 rolled_back 或 rollback_failed。
	result := resultFromTransaction(transaction)
	result.Failure = &updateengine.Failure{
		Code:    FailureTrialInterrupted,
		Message: "previous install trial was interrupted before commit",
		At:      time.Now().UTC(),
	}
	if journal == nil {
		result = projectRestoredResult(result, transaction, request, false)
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
		result.Failure.Code = FailureRollbackFailed
		result.Failure.Message = errors.Join(errors.New(result.Failure.Message), rollbackErr).Error()
		result = projectRestoredResult(result, transaction, request, false)
		transaction.ActiveVersion = result.ActiveVersion
		transaction.FallbackVersion = result.FallbackVersion
		completed, completeErr := store.Complete(transaction, updateengine.StateFailed, result)
		if completeErr != nil {
			return result, errors.Join(rollbackErr, completeErr)
		}
		return completed, rollbackErr
	}
	result = projectRestoredResult(result, transaction, request, true)
	transaction.ActiveVersion = result.ActiveVersion
	transaction.FallbackVersion = result.FallbackVersion
	return sealRolledBackInstall(store, transaction, result, nil)
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
		result.Failure = &updateengine.Failure{Code: string(phase) + "_failed", Message: err.Error(), At: time.Now().UTC()}
		if staged.Journal != nil {
			if rollbackErr := rollbackInstall(ctx, request, staged); rollbackErr != nil {
				result.Failure.Code = FailureRollbackFailed
				result.Failure.Message = errors.Join(err, rollbackErr).Error()
				result = projectRestoredResult(result, transaction, request, false)
				transaction.ActiveVersion = result.ActiveVersion
				transaction.FallbackVersion = result.FallbackVersion
				completed, completeErr := store.Complete(transaction, updateengine.StateFailed, result)
				if completeErr != nil {
					return result, errors.Join(err, rollbackErr, completeErr)
				}
				return completed, errors.Join(err, rollbackErr)
			}
			// 文件和服务已经回到安装前。Version 仍记录失败目标；
			// PublicURL / LocalMCPURL / PrivilegeMode 必须是恢复后的 known-good，不能留失败目标。
			result = projectRestoredResult(result, transaction, request, true)
			transaction.ActiveVersion = result.ActiveVersion
			transaction.FallbackVersion = result.FallbackVersion
			return sealRolledBackInstall(store, transaction, result, err)
		}
		transaction.Phase = phase
		result = projectRestoredResult(result, transaction, request, false)
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
	if err := store.WriteTransaction(transaction); err != nil {
		return fail(PhaseCommit, err, staged)
	}
	return commitPreparedInstall(store, transaction, result)
}

func installPhaseMayHaveMutatedFiles(phase Phase) bool {
	switch phase {
	case PhasePrepare, PhaseVerify, "":
		return false
	default:
		return true
	}
}

// commitPreparedInstall 先把 Windows trial pointer 收敛成 committed，再写权威事务。
// pointer 失败时 transaction/result 必须仍是 trial，journal 保留，恢复后可以安全重试完成。
func commitPreparedInstall(store *Store, transaction Transaction, result Result) (Result, error) {
	if err := commitWindowsActivePointer(transaction.InstallRoot, transaction.TransactionID); err != nil {
		return result, err
	}
	completed, err := store.Complete(transaction, updateengine.StateCommitted, result)
	if err != nil {
		return result, err
	}
	discardJournal(store.Root(), transaction.TransactionID)
	return completed, nil
}

// sealRolledBackInstall 只有 trial pointer 清掉之后才能写成 rolled_back 并丢 journal。
// 清理失败必须是 failed/rollback_failed，并保留 journal 作为恢复证据。
func sealRolledBackInstall(store *Store, transaction Transaction, result Result, original error) (Result, error) {
	if err := releaseWindowsTrialPointer(transaction.InstallRoot, transaction.TransactionID); err != nil {
		if result.Failure == nil {
			result.Failure = &updateengine.Failure{At: time.Now().UTC()}
		}
		result.Failure.Code = FailureRollbackFailed
		pointerErr := fmt.Errorf("release windows trial pointer: %w", err)
		if strings.TrimSpace(result.Failure.Message) == "" {
			result.Failure.Message = pointerErr.Error()
		} else {
			result.Failure.Message = errors.Join(errors.New(result.Failure.Message), pointerErr).Error()
		}
		transaction.Phase = PhaseRollback
		transaction.ActiveVersion = result.ActiveVersion
		transaction.FallbackVersion = result.FallbackVersion
		completed, completeErr := store.Complete(transaction, updateengine.StateFailed, result)
		if completeErr != nil {
			return result, errors.Join(original, err, completeErr)
		}
		return completed, errors.Join(original, err)
	}
	transaction.Phase = PhaseRollback
	transaction.ActiveVersion = result.ActiveVersion
	transaction.FallbackVersion = result.FallbackVersion
	completed, completeErr := store.Complete(transaction, updateengine.StateRolledBack, result)
	if completeErr != nil {
		return result, errors.Join(original, completeErr)
	}
	discardJournal(store.Root(), transaction.TransactionID)
	return completed, original
}

func removeWarning(warnings []string, remove string) []string {
	filtered := warnings[:0]
	for _, warning := range warnings {
		if warning != remove {
			filtered = append(filtered, warning)
		}
	}
	return filtered
}

func windowsUninstallAdapterWarnings(request Request) []string {
	if runtimeGOOS() != "windows" || request.PurgeData {
		return nil
	}
	// Engine 停进程不是整个产品卸载完成。Task/Registry/文件仍由 OS adapter 负责。
	return []string{"windows_adapter_pending"}
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
	if transaction.Action == ActionUninstall {
		current.Warnings = removeWarning(current.Warnings, "windows_adapter_pending")
	}
	if transaction.State == updateengine.StateCommitted && current.TransactionID == transaction.TransactionID {
		if err := commitWindowsActivePointer(transaction.InstallRoot, transaction.TransactionID); err != nil {
			return current, err
		}
		discardJournal(store.Root(), transaction.TransactionID)
		return current, nil
	}
	if transaction.State != updateengine.StateTrial {
		return current, fmt.Errorf("install commit 只能结束 trial，当前 state=%s transaction=%s", transaction.State, transaction.TransactionID)
	}
	return commitPreparedInstall(store, transaction, current)
}

func (engine Engine) abandon(store *Store, request Request) (Result, error) {
	transaction, current, err := bindInstallTransaction(store, request)
	if err != nil {
		return Result{}, fmt.Errorf("abandon: %w", err)
	}

	seal := func(state updateengine.State, code, message string, restored bool) (Result, error) {
		current.Failure = &updateengine.Failure{Code: code, Message: message, At: time.Now().UTC()}
		current = projectRestoredResult(current, transaction, request, restored)
		transaction.Phase = PhaseRollback
		transaction.ActiveVersion = current.ActiveVersion
		transaction.FallbackVersion = current.FallbackVersion
		if state == updateengine.StateRolledBack {
			return sealRolledBackInstall(store, transaction, current, nil)
		}
		completed, err := store.Complete(transaction, state, current)
		if err != nil {
			return current, err
		}
		return completed, nil
	}

	if request.RollbackFailed {
		return seal(updateengine.StateFailed, FailureExternalRollbackFailed, "OS adapter 回滚外部状态失败，权威事务不能写成 rolled_back", false)
	}
	if current.TransactionID == transaction.TransactionID && current.State == updateengine.StateRolledBack && current.Phase == PhaseRollback {
		if err := releaseWindowsTrialPointer(transaction.InstallRoot, transaction.TransactionID); err != nil {
			return seal(updateengine.StateFailed, FailureRollbackFailed, "release windows trial pointer: "+err.Error(), false)
		}
		discardJournal(store.Root(), transaction.TransactionID)
		return current, nil
	}
	if current.TransactionID == transaction.TransactionID && current.State == updateengine.StateFailed {
		if isExternalRollbackFailure(transaction) {
			// 明确恢复入口：操作者已经修好 Task/Registry/服务后，再次 abandon（不带 --rollback-failed）
			// 才能把外部失败收敛成 rolled_back。Engine journal 成功不能代替这一步。
			return seal(updateengine.StateRolledBack, FailureAbandoned, "operator confirmed external rollback is complete", true)
		}
		return current, nil
	}
	switch transaction.State {
	case updateengine.StateCommitted, updateengine.StateTrial, updateengine.StateStaged, updateengine.StateRollingBack, updateengine.StateRolledBack:
	default:
		return current, fmt.Errorf("install abandon 不能处理 state=%s", transaction.State)
	}
	return seal(updateengine.StateRolledBack, FailureAbandoned, "OS adapter 已完成外部回滚，权威状态撤销为 rolled_back", true)
}

func (engine Engine) uninstall(ctx context.Context, store *Store, request Request) (Result, error) {
	platform := currentPlatform()
	sourceVersion := existingVersion(request)
	transaction, err := newTransaction(request, platform, sourceVersion)
	if err != nil {
		return Result{}, err
	}
	transaction.Phase = PhaseRollback
	if err := store.WriteTransaction(transaction); err != nil {
		return Result{}, err
	}

	fail := func(err error) (Result, error) {
		result := Result{
			Failure:       &updateengine.Failure{Code: FailureUninstallFailed, Message: err.Error(), At: time.Now().UTC()},
			Version:       request.Version,
			ActiveVersion: sourceVersion,
			Healthy:       false,
		}
		completed, completeErr := store.Complete(transaction, updateengine.StateFailed, result)
		if completeErr != nil {
			return result, errors.Join(err, completeErr)
		}
		return completed, err
	}

	if err := uninstallPlatform(ctx, request); err != nil {
		return fail(err)
	}
	if request.PurgeConfig && !request.PurgeData {
		if err := purgeInstallConfig(request); err != nil {
			return fail(err)
		}
	}
	if request.PurgeData {
		if err := purgeInstallData(request); err != nil {
			return fail(err)
		}
		// runtime-root 已经删掉，不能再写 journal 把卸载成功伪装成“状态还在”。
		now := time.Now().UTC()
		return Result{
			SchemaVersion: SchemaVersion,
			TransactionID: transaction.TransactionID,
			Platform:      platform,
			Action:        ActionUninstall,
			State:         updateengine.StateCommitted,
			Phase:         PhaseCommit,
			Version:       request.Version,
			ActiveVersion: sourceVersion,
			Healthy:       false,
			StartedAt:     transaction.StartedAt,
			CompletedAt:   now,
		}, nil
	}

	result := Result{
		Version:       request.Version,
		ActiveVersion: sourceVersion,
		Healthy:       false,
		Warnings:      windowsUninstallAdapterWarnings(request),
	}
	if request.DeferCommit {
		// Windows OS adapter 还要删 Task/Registry/文件。现在只能停在 trial，
		// 不能把 Engine 停进程写成“整个产品已经卸载完成”。
		transaction.State = updateengine.StateTrial
		transaction.Phase = PhaseCommit
		if err := store.WriteTransaction(transaction); err != nil {
			return result, err
		}
		result.SchemaVersion = SchemaVersion
		result.TransactionID = transaction.TransactionID
		result.Platform = platform
		result.Action = ActionUninstall
		result.State = updateengine.StateTrial
		result.Phase = PhaseCommit
		result.StartedAt = transaction.StartedAt
		if err := store.WriteResult(result); err != nil {
			return result, err
		}
		return result, nil
	}

	completed, err := store.Complete(transaction, updateengine.StateCommitted, result)
	if err != nil {
		return result, err
	}
	discardJournal(store.Root(), transaction.TransactionID)
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
		if version := versionFromInstallTransaction(transaction); version != "" {
			return version
		}
	}
	current, err := store.ReadCurrentResult()
	if err == nil {
		if version := versionFromInstallResult(current); version != "" {
			return version
		}
	}
	return windowsCommittedGeneration(request)
}

func versionFromInstallTransaction(transaction Transaction) string {
	switch transaction.Action {
	case ActionUninstall:
		// 卸载终态不是一次成功安装。known-good 是卸载前仍保留的程序版本，
		// 不能把 target_version=unknown 当成下一次 install 的 source。
		return knownInstallVersion(transaction.SourceVersion, transaction.ActiveVersion)
	}
	switch transaction.State {
	case updateengine.StateCommitted:
		if version := knownInstallVersion(transaction.ActiveVersion, transaction.TargetVersion); version != "" {
			return version
		}
	case updateengine.StateRolledBack, updateengine.StateFailed, updateengine.StateTrial, updateengine.StateStaged, updateengine.StateRollingBack:
		return knownInstallVersion(transaction.SourceVersion)
	}
	return ""
}

func versionFromInstallResult(result Result) string {
	if result.Action == ActionUninstall {
		return knownInstallVersion(result.ActiveVersion)
	}
	if result.State != updateengine.StateCommitted {
		return ""
	}
	return knownInstallVersion(result.ActiveVersion, result.Version)
}

func knownInstallVersion(candidates ...string) string {
	for _, candidate := range candidates {
		version := strings.TrimSpace(candidate)
		if version != "" && version != "unknown" && version != "uninstalled" {
			return version
		}
	}
	return ""
}

func isExternalRollbackFailure(transaction Transaction) bool {
	if transaction.State != updateengine.StateFailed || transaction.Failure == nil {
		return false
	}
	switch transaction.Failure.Code {
	case FailureExternalRollbackFailed:
		return true
	case FailureRollbackFailed:
		// 兼容本轮之前 abandon --rollback-failed 写入的 rollback_failed + OS adapter 文案。
		return strings.Contains(transaction.Failure.Message, "OS adapter")
	default:
		return false
	}
}

func projectRestoredResult(result Result, transaction Transaction, request Request, restored bool) Result {
	result.Healthy = false
	result.ActiveVersion = transaction.SourceVersion
	result.FallbackVersion = transaction.SourceVersion
	result.LocalMCPURL = ""
	result.PublicURL = ""
	result.PrivilegeMode = ""
	if !restored {
		return result
	}
	activated, err := readActivatedInstall(request)
	if err != nil {
		return result
	}
	result.LocalMCPURL = activated.LocalMCPURL
	result.PublicURL = activated.PublicURL
	result.PrivilegeMode = activated.PrivilegeMode
	return result
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
