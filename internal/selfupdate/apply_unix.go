//go:build !windows

package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type desktopUpdateOutcome struct {
	OK             bool   `json:"ok"`
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	Message        string `json:"message"`
}

type desktopUpdateTransaction interface {
	Install(context.Context) error
	Restore(context.Context) error
	Finish(context.Context, desktopUpdateOutcome) error
	Commit() error
}

func applyPlatformUpdate(ctx context.Context, request applyRequest) (applyResult, error) {
	if request.DesktopOnly {
		return applyDesktopOnlyUpdate(ctx, request)
	}
	service := detectManagedService(ctx, request.CurrentPath)
	platformCandidates := platformHealthCandidates(ctx, request.CurrentPath)
	var candidates []string
	if service != nil && len(platformCandidates) > 0 {
		// 托管服务必须验证自身配置的地址，不能被同机其他 AgentDock 实例的默认端口误导。
		candidates = uniqueStrings(platformCandidates)
	} else {
		candidates = uniqueStrings(append(platformCandidates, healthCandidates(request.CurrentPath)...))
	}
	if healthyURL := findHealthyURL(ctx, candidates); healthyURL != "" {
		candidates = []string{healthyURL}
	}

	desktopUpdate, err := prepareDesktopUpdate(
		ctx,
		request.DesktopTargetPath,
		request.DesktopStagedPath,
		request.TargetVersion,
	)
	if err != nil {
		return applyResult{}, fmt.Errorf("准备 macOS 桌面更新失败，当前版本未被修改: %w", err)
	}

	backupPath, err := platformBackupPath(request.CurrentPath)
	if err != nil {
		return applyResult{}, fmt.Errorf("准备更新备份路径失败，当前版本未被修改: %w", err)
	}
	newPath := request.CurrentPath + ".new"
	if err := installReplacement(request.StagedPath, request.CurrentPath, newPath, backupPath); err != nil {
		return applyResult{}, fmt.Errorf("安装新版本失败，当前版本未被修改: %w", err)
	}
	previousPID := managedServicePID(ctx, service)
	rollback := func(cause error) error {
		failedPID := managedServicePID(ctx, service)
		rollbackErrors := make([]string, 0, 4)
		if desktopUpdate != nil {
			if restoreErr := desktopUpdate.Restore(ctx); restoreErr != nil {
				rollbackErrors = append(rollbackErrors, "恢复旧控制面板失败: "+restoreErr.Error())
			}
		}
		if restoreErr := restoreBackup(request.CurrentPath, backupPath); restoreErr != nil {
			rollbackErrors = append(rollbackErrors, "恢复旧核心失败: "+restoreErr.Error())
		} else if service != nil {
			if restartErr := restartManagedService(ctx, service, request.CurrentPath, failedPID, candidates, request.CurrentVersion); restartErr != nil {
				rollbackErrors = append(rollbackErrors, "重新启动 "+service.Name()+" 失败: "+restartErr.Error())
			}
		}
		if desktopUpdate != nil {
			outcome := desktopUpdateOutcome{
				OK:             false,
				CurrentVersion: request.CurrentVersion,
				TargetVersion:  request.TargetVersion,
				Message:        cause.Error() + "；已自动恢复旧版本。",
			}
			if finishErr := desktopUpdate.Finish(ctx, outcome); finishErr != nil {
				rollbackErrors = append(rollbackErrors, "重新打开旧控制面板失败: "+finishErr.Error())
			}
		}
		if len(rollbackErrors) > 0 {
			return fmt.Errorf("%v；自动恢复不完整: %s", cause, strings.Join(rollbackErrors, "；"))
		}
		return fmt.Errorf("%v；已自动恢复旧版本", cause)
	}

	if err := signLocalReplacement(ctx, request.CurrentPath); err != nil {
		return applyResult{}, rollback(fmt.Errorf("新版本本地签名失败: %w", err))
	}
	if err := verifyBinaryVersion(ctx, request.CurrentPath, request.TargetVersion); err != nil {
		return applyResult{}, rollback(fmt.Errorf("替换后的二进制验证失败: %w", err))
	}
	if service != nil {
		fmt.Fprintf(request.Output, "正在重启 %s...\n", service.Name())
		if err := restartManagedService(ctx, service, request.CurrentPath, previousPID, candidates, request.TargetVersion); err != nil {
			return applyResult{}, rollback(fmt.Errorf("重启 %s 或验证新进程失败: %w", service.Name(), err))
		}
		fmt.Fprintln(request.Output, "健康检查通过")
	}

	if desktopUpdate != nil {
		fmt.Fprintln(request.Output, "正在更新 macOS 控制面板...")
		reportUpdateStage(request.Progress, UpdateStageInstalling, request.CurrentVersion, request.TargetVersion, "")
		if err := desktopUpdate.Install(ctx); err != nil {
			return applyResult{}, rollback(fmt.Errorf("安装 macOS 控制面板失败: %w", err))
		}
	}

	fmt.Fprintln(request.Output, "正在更新官方核心 Skill...")
	reportUpdateStage(request.Progress, UpdateStageUpdatingSkill, request.CurrentVersion, request.TargetVersion, "")
	if err := bootstrapBundledSkills(ctx, request.CurrentPath, request.BundlePath, request.Output); err != nil {
		return applyResult{}, rollback(err)
	}

	if desktopUpdate != nil {
		reportUpdateStage(request.Progress, UpdateStageRestarting, request.CurrentVersion, request.TargetVersion, "")
		outcome := desktopUpdateOutcome{
			OK:             true,
			CurrentVersion: request.CurrentVersion,
			TargetVersion:  request.TargetVersion,
			Message:        fmt.Sprintf("AgentDock 已从 %s 更新到 %s。", request.CurrentVersion, request.TargetVersion),
		}
		if err := desktopUpdate.Finish(ctx, outcome); err != nil {
			return applyResult{}, rollback(fmt.Errorf("重新打开新版 macOS 控制面板失败: %w", err))
		}
		if err := desktopUpdate.Commit(); err != nil {
			fmt.Fprintf(request.Output, "警告：清理 macOS 控制面板更新备份失败: %v\n", err)
		}
	}
	return applyResult{Restarted: service != nil}, nil
}

func applyDesktopOnlyUpdate(ctx context.Context, request applyRequest) (applyResult, error) {
	if err := validateDesktopUpdateCoordination(); err != nil {
		return applyResult{}, err
	}
	desktopUpdate, err := prepareDesktopUpdate(
		ctx,
		request.DesktopTargetPath,
		request.DesktopStagedPath,
		request.TargetVersion,
	)
	if err != nil {
		return applyResult{}, fmt.Errorf("准备 AgentDock.app 更新失败，当前版本未被修改: %w", err)
	}
	if desktopUpdate == nil {
		return applyResult{}, errors.New("macOS 桌面更新事务不可用")
	}

	rollback := func(cause error) error {
		rollbackErrors := make([]string, 0, 2)
		if restoreErr := desktopUpdate.Restore(ctx); restoreErr != nil {
			rollbackErrors = append(rollbackErrors, "恢复旧 AgentDock.app 失败: "+restoreErr.Error())
		}
		outcome := desktopUpdateOutcome{
			OK:             false,
			CurrentVersion: request.CurrentVersion,
			TargetVersion:  request.TargetVersion,
			Message:        cause.Error() + "；已自动恢复旧版本。",
		}
		if finishErr := desktopUpdate.Finish(ctx, outcome); finishErr != nil {
			rollbackErrors = append(rollbackErrors, "重新打开旧 AgentDock.app 失败: "+finishErr.Error())
		}
		if len(rollbackErrors) > 0 {
			return fmt.Errorf("%v；自动恢复不完整: %s", cause, strings.Join(rollbackErrors, "；"))
		}
		return fmt.Errorf("%v；已自动恢复旧版本", cause)
	}

	fmt.Fprintln(request.Output, "正在替换 AgentDock.app...")
	reportUpdateStage(request.Progress, UpdateStageInstalling, request.CurrentVersion, request.TargetVersion, "")
	if err := desktopUpdate.Install(ctx); err != nil {
		return applyResult{}, rollback(err)
	}

	installedCore := filepath.Join(request.DesktopTargetPath, "Contents", "Helpers", "agentdock")
	installedSkills := filepath.Join(request.DesktopTargetPath, "Contents", "Resources", "core-skills")
	fmt.Fprintln(request.Output, "正在更新官方核心 Skill...")
	reportUpdateStage(request.Progress, UpdateStageUpdatingSkill, request.CurrentVersion, request.TargetVersion, "")
	if err := bootstrapBundledSkills(ctx, installedCore, installedSkills, request.Output); err != nil {
		return applyResult{}, rollback(err)
	}

	outcome := desktopUpdateOutcome{
		OK:             true,
		CurrentVersion: request.CurrentVersion,
		TargetVersion:  request.TargetVersion,
		Message:        fmt.Sprintf("AgentDock 已从 %s 更新到 %s。", request.CurrentVersion, request.TargetVersion),
	}
	reportUpdateStage(request.Progress, UpdateStageRestarting, request.CurrentVersion, request.TargetVersion, "")
	if err := desktopUpdate.Finish(ctx, outcome); err != nil {
		return applyResult{}, rollback(fmt.Errorf("新版 AgentDock.app 未完成更新接管: %w", err))
	}
	if err := desktopUpdate.Commit(); err != nil {
		fmt.Fprintf(request.Output, "警告：清理 AgentDock.app 更新备份失败: %v\n", err)
	}
	return applyResult{}, nil
}

func validateDesktopUpdateCoordination() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("解析用户目录失败: %w", err)
	}
	path := filepath.Join(home, "Library", "Application Support", "AgentDock", "update-services.json")
	info, err := os.Lstat(path)
	if err != nil {
		return errors.New("macOS 桌面更新必须从 AgentDock 控制面板发起")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("macOS 更新服务状态文件不安全: %s", path)
	}
	return nil
}

func managedServicePID(ctx context.Context, service managedService) int {
	verifier, ok := service.(managedServiceVerifier)
	if !ok {
		return 0
	}
	pid, err := verifier.CurrentPID(ctx)
	if err != nil {
		return 0
	}
	return pid
}

func restartManagedService(
	ctx context.Context,
	service managedService,
	targetPath string,
	previousPID int,
	healthCandidates []string,
	targetVersion string,
) error {
	if err := service.Restart(ctx); err != nil {
		return err
	}
	if verifier, ok := service.(managedServiceVerifier); ok {
		if err := verifier.WaitForRestart(ctx, targetPath, previousPID, 30*time.Second); err != nil {
			return err
		}
	}
	if err := waitForVersion(ctx, healthCandidates, targetVersion, 30*time.Second); err != nil {
		return err
	}
	return nil
}

func installReplacement(stagedPath, targetPath, newPath, backupPath string) error {
	info, err := os.Stat(targetPath)
	if err != nil {
		return fmt.Errorf("读取当前二进制失败: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("当前二进制不是普通文件: %s", targetPath)
	}
	if err := os.Remove(newPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清理临时文件失败: %w", err)
	}
	if err := copyFile(stagedPath, newPath, info.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(newPath)
		return fmt.Errorf("清理旧备份失败: %w", err)
	}
	if err := copyFile(targetPath, backupPath, info.Mode().Perm()); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("备份当前二进制失败: %w", err)
	}

	// 新文件先落在目标同目录，再用 rename 原子覆盖；即使替换瞬间崩溃，目标路径也不会消失。
	if err := os.Rename(newPath, targetPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("原子替换新二进制失败: %w", err)
	}
	if directory, err := os.Open(filepath.Dir(targetPath)); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func copyFile(sourcePath, targetPath string, mode os.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("打开新版本二进制失败: %w", err)
	}
	defer source.Close()
	target, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("创建同目录更新文件失败: %w", err)
	}
	copied := false
	defer func() {
		_ = target.Close()
		if !copied {
			_ = os.Remove(targetPath)
		}
	}()
	if _, err := io.Copy(target, source); err != nil {
		return fmt.Errorf("复制新版本二进制失败: %w", err)
	}
	if err := target.Sync(); err != nil {
		return fmt.Errorf("同步新版本二进制失败: %w", err)
	}
	if err := target.Close(); err != nil {
		return fmt.Errorf("关闭新版本二进制失败: %w", err)
	}
	copied = true
	return nil
}

func restoreBackup(targetPath, backupPath string) error {
	return os.Rename(backupPath, targetPath)
}
