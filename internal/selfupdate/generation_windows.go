//go:build windows

package selfupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/uvwt/agentdock/internal/desktopruntime"
	"github.com/uvwt/agentdock/internal/fs/atomicfile"
	"github.com/uvwt/agentdock/internal/updateengine"
)

func windowsGenerationInstall(currentPath string) (root string, version string, ok bool) {
	if !strings.EqualFold(filepath.Base(currentPath), updateengine.GenerationCoreName) {
		return "", "", false
	}
	generationDir := filepath.Dir(currentPath)
	versionsDir := filepath.Dir(generationDir)
	if !strings.EqualFold(filepath.Base(versionsDir), "versions") {
		return "", "", false
	}
	version = normalizeVersion(filepath.Base(generationDir))
	if version == "" {
		return "", "", false
	}
	return filepath.Dir(versionsDir), version, true
}

func applyWindowsGenerationUpdate(ctx context.Context, request applyRequest) (applyResult, error) {
	root, sourceVersion, ok := windowsGenerationInstall(request.CurrentPath)
	if !ok {
		return applyResult{}, errors.New("当前 Windows 核心不属于 generation 布局")
	}
	if sourceVersion != normalizeVersion(request.CurrentVersion) {
		return applyResult{}, fmt.Errorf("当前 generation %s 与运行版本 %s 不一致", sourceVersion, normalizeVersion(request.CurrentVersion))
	}
	layout, err := updateengine.NewWindowsLayout(root)
	if err != nil {
		return applyResult{}, err
	}
	store, err := updateengine.NewStore(root)
	if err != nil {
		return applyResult{}, err
	}
	active, err := store.ReadActive()
	if err != nil {
		return applyResult{}, fmt.Errorf("读取 Windows active generation 失败: %w", err)
	}
	if active.State != updateengine.StateCommitted || normalizeVersion(active.ActiveVersion) != sourceVersion {
		return applyResult{}, fmt.Errorf("Windows active generation 尚未处于 committed 状态: active=%s state=%s", active.ActiveVersion, active.State)
	}
	manifest, err := desktopruntime.LoadForBinary(request.CurrentPath)
	if err != nil {
		return applyResult{}, fmt.Errorf("读取 Windows runtime manifest 失败: %w", err)
	}

	coreWasRunning, err := desktopruntime.BinaryProcessRunning(layout.GenerationCore(sourceVersion))
	if err != nil {
		return applyResult{}, fmt.Errorf("检查 Windows Core 运行状态失败: %w", err)
	}
	trayWasRunning, err := desktopruntime.BinaryProcessRunning(layout.GenerationTray(sourceVersion))
	if err != nil {
		return applyResult{}, fmt.Errorf("检查 Windows Tray 运行状态失败: %w", err)
	}
	tunnelWasRunning, err := windowsTunnelRunning(ctx, root)
	if err != nil {
		return applyResult{}, fmt.Errorf("检查 Windows Tunnel 运行状态失败: %w", err)
	}

	transaction, err := updateengine.NewTransaction("windows", sourceVersion, request.TargetVersion)
	if err != nil {
		return applyResult{}, err
	}
	transaction.ActiveVersion = sourceVersion
	transaction.FallbackVersion = active.FallbackVersion
	transaction.Windows = &updateengine.WindowsPlan{
		InstallRoot:       root,
		SourceGeneration:  layout.GenerationDir(sourceVersion),
		TargetGeneration:  layout.GenerationDir(request.TargetVersion),
		HealthURLs:        []string{manifest.HealthURL()},
		TaskName:          manifest.AgentDockTaskName,
		PrivilegeMode:     manifest.PrivilegeMode,
		CoreWasRunning:    coreWasRunning,
		TrayWasRunning:    trayWasRunning,
		TunnelWasRunning:  tunnelWasRunning,
		ProgressUIHandoff: strings.TrimSpace(os.Getenv("AGENTDOCK_UPDATE_UI_HANDOFF")) == "1",
	}

	fmt.Fprintf(request.Output, "正在暂存 Windows generation %s...\n", normalizeVersion(request.TargetVersion))
	reportUpdateStage(request.Progress, UpdateStageInstalling, request.CurrentVersion, request.TargetVersion, "generation")
	if err := stageWindowsGeneration(ctx, layout, transaction.TransactionID, request); err != nil {
		return applyResult{}, err
	}
	if err := store.WriteTransaction(transaction); err != nil {
		return applyResult{}, fmt.Errorf("写入 Windows 更新事务失败: %w", err)
	}

	// Arbiter 必须从 source generation 直接启动。side-by-side 不覆盖当前 update CLI，
	// 因此调用者会一直等待 terminal result，父进程退出不再冒充更新成功。
	sourceArbiter := layout.GenerationArbiter(sourceVersion)
	reportUpdateStage(request.Progress, UpdateStageRestarting, request.CurrentVersion, request.TargetVersion, "arbiter")
	command := exec.CommandContext(ctx, sourceArbiter, "--root", root, "--transaction-id", transaction.TransactionID)
	command.Dir = root
	output, runErr := command.CombinedOutput()
	if len(bytes.TrimSpace(output)) > 0 {
		fmt.Fprintf(request.Output, "%s\n", bytes.TrimSpace(output))
	}
	result, resultErr := store.ReadResult(transaction.TransactionID)
	if resultErr != nil {
		if runErr != nil {
			return applyResult{}, fmt.Errorf("Windows Arbiter 失败且未写入可恢复的最终结果: %w: %v", runErr, resultErr)
		}
		return applyResult{}, fmt.Errorf("Windows Arbiter 未写入最终结果: %w", resultErr)
	}
	if result.State != updateengine.StateCommitted {
		return applyResult{}, fmt.Errorf("Windows update 未提交: %s", terminalUpdateMessage(result))
	}
	// The terminal journal/result is authoritative. The Arbiter process can still exit non-zero
	// if a derived result projection failed immediately after the durable commit; ReadResult above
	// repairs that projection from transaction.json, so surfacing the stale process error would
	// incorrectly report a committed update as failed.

	// Stable shims are deliberately outside the online update transaction. The CUI shim is
	// the parent that is waiting for this generation update to finish, so Windows may keep it
	// locked until we return. Keep the shim ABI tiny/stable and refresh it only through Setup/
	// repair, where no shim process needs to replace itself.
	if err := atomicfile.Write(filepath.Join(root, windowsDesktopVersionFile), []byte(normalizeVersion(request.TargetVersion)+"\n"), 0o600); err != nil {
		fmt.Fprintf(request.Output, "警告：写入 Windows 桌面版本标记失败: %v\n", err)
	}
	if err := bootstrapBundledSkills(ctx, layout.GenerationCore(request.TargetVersion), layout.GenerationSkills(request.TargetVersion), request.Output); err != nil {
		fmt.Fprintf(request.Output, "警告：新版本已提交，但官方核心 Skill 同步失败: %v\n", err)
	}
	garbageCollectWindowsGenerations(layout, normalizeVersion(request.TargetVersion), sourceVersion)
	return applyResult{Restarted: coreWasRunning}, nil
}

func stageWindowsGeneration(ctx context.Context, layout updateengine.WindowsLayout, transactionID string, request applyRequest) error {
	if strings.TrimSpace(request.DesktopStagedPath) == "" {
		return errors.New("Windows generation 更新缺少桌面归档内容")
	}
	targetVersion := normalizeVersion(request.TargetVersion)
	targetDir := layout.GenerationDir(targetVersion)
	stagingDir := filepath.Join(layout.VersionsDir(), ".staging-"+transactionID)
	if err := os.RemoveAll(stagingDir); err != nil {
		return fmt.Errorf("清理 Windows generation 暂存目录失败: %w", err)
	}
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return fmt.Errorf("创建 Windows generation 暂存目录失败: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	files := map[string]string{
		request.StagedPath: filepath.Join(stagingDir, updateengine.GenerationCoreName),
		filepath.Join(request.DesktopStagedPath, "agentdock-tray.exe"):    filepath.Join(stagingDir, updateengine.GenerationTrayName),
		filepath.Join(request.DesktopStagedPath, "agentdock-arbiter.exe"): filepath.Join(stagingDir, updateengine.GenerationArbiterName),
	}
	for source, target := range files {
		info, err := os.Stat(source)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("Windows generation 文件缺失: %s", source)
		}
		if err := copyFileWindows(source, target); err != nil {
			return fmt.Errorf("暂存 Windows generation 文件 %s 失败: %w", filepath.Base(target), err)
		}
	}
	if err := copyDirectoryWindows(request.BundlePath, filepath.Join(stagingDir, "core-skills")); err != nil {
		return fmt.Errorf("暂存 Windows generation 核心 Skill 失败: %w", err)
	}
	if err := copyDirectoryWindows(
		filepath.Join(request.DesktopStagedPath, "wsl-helper"),
		filepath.Join(stagingDir, "wsl-helper"),
	); err != nil {
		return fmt.Errorf("暂存 Windows generation WSL helper 失败: %w", err)
	}
	for _, relative := range []string{
		"wsl-helper/manifest.json",
		"wsl-helper/agentdock-wsl-helper-linux-amd64",
		"wsl-helper/agentdock-wsl-helper-linux-arm64",
	} {
		path := filepath.Join(stagingDir, filepath.FromSlash(relative))
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("Windows generation WSL helper 文件缺失: %s", relative)
		}
	}
	if err := verifyBinaryVersion(ctx, filepath.Join(stagingDir, updateengine.GenerationCoreName), targetVersion); err != nil {
		return fmt.Errorf("Windows generation 核心版本验证失败: %w", err)
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("清理旧的目标 generation 失败: %w", err)
	}
	from, err := windows.UTF16PtrFromString(stagingDir)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(targetDir)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return fmt.Errorf("发布 Windows generation 失败: %w", err)
	}
	cleanup = false
	return nil
}

func windowsTunnelRunning(ctx context.Context, runtimeRoot string) (bool, error) {
	var stdout, stderr bytes.Buffer
	if err := desktopruntime.RunTunnelCommand(ctx, []string{"status", "--runtime-root", runtimeRoot}, &stdout, &stderr); err != nil {
		return false, err
	}
	var status desktopruntime.TunnelStatus
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		return false, fmt.Errorf("解析 Tunnel 状态失败: %w", err)
	}
	return status.Running, nil
}

func garbageCollectWindowsGenerations(layout updateengine.WindowsLayout, keepVersions ...string) {
	keep := make(map[string]struct{}, len(keepVersions))
	for _, version := range keepVersions {
		keep[strings.ToLower(normalizeVersion(version))] = struct{}{}
	}
	entries, err := os.ReadDir(layout.VersionsDir())
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if _, ok := keep[strings.ToLower(normalizeVersion(entry.Name()))]; ok {
			continue
		}
		_ = os.RemoveAll(filepath.Join(layout.VersionsDir(), entry.Name()))
	}
}
