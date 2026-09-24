//go:build windows

package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/uvwt/agentdock/internal/desktopruntime"
	"github.com/uvwt/agentdock/internal/installer"
	"github.com/uvwt/agentdock/internal/updateengine"
)

const (
	windowsLegacyMigrationCommand   = "__migrate-windows-legacy-layout"
	windowsLegacyMigrationMutexName = `Local\AgentDockLegacyMigration`
)

type windowsLegacyMigrationPlan struct {
	ParentPID        int    `json:"parent_pid"`
	RuntimeRoot      string `json:"runtime_root"`
	CorePath         string `json:"core_path"`
	TrayPath         string `json:"tray_path"`
	PayloadDir       string `json:"payload_dir"`
	Version          string `json:"version"`
	CoreWasRunning   bool   `json:"core_was_running"`
	TrayWasRunning   bool   `json:"tray_was_running"`
	TunnelWasRunning bool   `json:"tunnel_was_running"`
	CleanupRoot      string `json:"cleanup_root"`
}

// windowsLegacyMigrationNeeded 识别受管桌面安装仍未完成的一次性 legacy bridge。
// 正常入口是 flat stable Core；如果进程在 stable entry 切换中途退出，generation Core
// 也会凭已安装的 compatibility manager 重新进入迁移。portable CLI 始终不参与。
func windowsLegacyMigrationNeeded(opts options) bool {
	if strings.TrimSpace(opts.DesktopTargetPath) == "" ||
		normalizeVersion(opts.CurrentVersion) == "" ||
		normalizeVersion(opts.CurrentVersion) == "vdev" {
		return false
	}
	layout, err := updateengine.NewWindowsLayout(opts.DesktopTargetPath)
	if err != nil {
		return false
	}
	if sameWindowsPath(opts.ExecutablePath, layout.CoreShim()) {
		return true
	}

	// PrepareWindowsLegacyGeneration 会先提交 source pointer，再替换 stable entry。
	// 如果 helper 在只替换 Core，或两个 shim 都替换但尚未清理时退出，下一次运行的
	// Core 已经来自 generation。旧 updater 安装的 compatibility manager 因此兼作
	// 持久完成标记：只有迁移到达终态并删除它后，bridge 才算真正结束。
	root, version, generationAware := windowsGenerationInstall(opts.ExecutablePath)
	if !generationAware ||
		!sameWindowsPath(root, opts.DesktopTargetPath) ||
		normalizeVersion(version) != normalizeVersion(opts.CurrentVersion) {
		return false
	}
	manager := filepath.Join(opts.DesktopTargetPath, "installer", "manage-windows.ps1")
	info, err := os.Stat(manager)
	return err == nil && info.Mode().IsRegular()
}

func windowsLegacyMigrationReady(opts options) bool {
	return windowsLegacyMigrationNeeded(opts) &&
		normalizeVersion(opts.DesktopCurrentVersion) == normalizeVersion(opts.CurrentVersion)
}

// windowsLegacyMigrationInProgress 只探测当前登录会话里是否已有迁移 helper。
// helper 在失败恢复并重启 Core 的整个过程都持有该 mutex，因此新 Core 可以据此
// 避免在同一次恢复过程中立刻再发起第二轮迁移；helper 退出后下次启动仍可正常重试。
func windowsLegacyMigrationInProgress() (bool, error) {
	mutexName, err := windows.UTF16PtrFromString(windowsLegacyMigrationMutexName)
	if err != nil {
		return false, err
	}
	mutex, err := windows.CreateMutex(nil, false, mutexName)
	if mutex != 0 {
		defer windows.CloseHandle(mutex)
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("探测 Windows legacy migration 互斥锁失败: %w", err)
	}
	return false, nil
}

// runWindowsLegacyLayoutMigration 是旧 updater -> 新架构的一次性桥。
// 旧 0.8.2/0.8.3 updater 会先把新 Core/Tray 按 flat 布局落盘并通过健康检查；
// 新 Core 启动后再在后台下载“与自身同版本”的正式 Release，只取 migration
// infrastructure 建立 source generation。这里绝不顺带升级到另一个版本。
func runWindowsLegacyLayoutMigration(
	ctx context.Context,
	opts options,
	output io.Writer,
	localArchivePath string,
	localChecksumPath string,
) error {
	if !windowsLegacyMigrationNeeded(opts) {
		return nil
	}
	if opts.HTTPClient == nil {
		return errors.New("Windows legacy migration HTTP 客户端不能为空")
	}
	if output == nil {
		output = io.Discard
	}

	currentVersion := normalizeVersion(opts.CurrentVersion)
	archiveName, executableName, err := platformAssetNames(opts.GOOS, opts.GOARCH)
	if err != nil {
		return err
	}

	var archiveData, checksumData []byte
	if strings.TrimSpace(localArchivePath) != "" || strings.TrimSpace(localChecksumPath) != "" {
		if strings.TrimSpace(localArchivePath) == "" || strings.TrimSpace(localChecksumPath) == "" {
			return errors.New("本地 legacy migration 必须同时提供 archive 与 checksum")
		}
		archiveData, err = readBoundedLocalFile(localArchivePath, maxDesktopArchiveBytes)
		if err != nil {
			return fmt.Errorf("读取 Windows legacy migration archive: %w", err)
		}
		checksumData, err = readBoundedLocalFile(localChecksumPath, 1<<20)
		if err != nil {
			return fmt.Errorf("读取 Windows legacy migration checksum: %w", err)
		}
	} else {
		latest, fetchErr := fetchLatestRelease(ctx, opts.HTTPClient, opts.ReleaseAPI)
		if fetchErr != nil {
			return fetchErr
		}
		if normalizeVersion(latest.TagName) != currentVersion {
			// 当前版本尚未（或已经不再）是 latest 时不在服务启动阶段改动布局。
			// flat Core 会在后续启动继续重试；显式 update 仍保持原有升级行为。
			return nil
		}
		archiveAsset, ok := findAsset(latest.Assets, archiveName)
		if !ok {
			return fmt.Errorf("Release %s 缺少 Windows legacy migration 文件 %s", currentVersion, archiveName)
		}
		checksumAsset, ok := findAsset(latest.Assets, archiveName+".sha256")
		if !ok {
			return fmt.Errorf("Release %s 缺少 Windows legacy migration 校验文件 %s.sha256", currentVersion, archiveName)
		}
		archiveData, err = download(ctx, opts.HTTPClient, archiveAsset.URL, maxDesktopArchiveBytes)
		if err != nil {
			return fmt.Errorf("下载 Windows legacy migration 文件失败: %w", err)
		}
		checksumData, err = download(ctx, opts.HTTPClient, checksumAsset.URL, 1<<20)
		if err != nil {
			return fmt.Errorf("下载 Windows legacy migration 校验文件失败: %w", err)
		}
	}
	if err := verifyChecksum(archiveData, checksumData); err != nil {
		return fmt.Errorf("Windows legacy migration 文件校验失败: %w", err)
	}

	tempRoot, err := os.MkdirTemp("", "agentdock-legacy-migration-*")
	if err != nil {
		return fmt.Errorf("创建 Windows legacy migration 临时目录失败: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tempRoot)
		}
	}()

	// 先验证 Release Core 与当前已运行 Core 完全同版本，避免服务启动修复误用另一个
	// Release 的 shim/arbiter infrastructure。
	binaryData, err := extractExecutable(archiveData, opts.GOOS, executableName)
	if err != nil {
		return fmt.Errorf("解压 Windows legacy migration Core 失败: %w", err)
	}
	verificationBinary := filepath.Join(tempRoot, executableName)
	if err := os.WriteFile(verificationBinary, binaryData, 0o755); err != nil {
		return err
	}
	if err := opts.VerifyBinary(ctx, verificationBinary, currentVersion); err != nil {
		return fmt.Errorf("Windows legacy migration Release 版本验证失败: %w", err)
	}

	payloadDir, err := opts.ExtractDesktop(ctx, archiveData, tempRoot, currentVersion)
	if err != nil {
		return fmt.Errorf("解压 Windows legacy migration payload 失败: %w", err)
	}
	if err := validateWindowsLegacyMigrationPayload(payloadDir); err != nil {
		return err
	}

	manifest, err := desktopruntime.LoadForBinary(opts.ExecutablePath)
	if err != nil {
		return fmt.Errorf("读取 Windows legacy runtime manifest 失败: %w", err)
	}
	coreWasRunning, err := desktopruntime.BinaryProcessRunning(opts.ExecutablePath)
	if err != nil {
		return fmt.Errorf("检查 legacy Core 运行状态失败: %w", err)
	}
	trayWasRunning, err := desktopruntime.BinaryProcessRunning(manifest.TrayBinary)
	if err != nil {
		return fmt.Errorf("检查 legacy Tray 运行状态失败: %w", err)
	}
	tunnelWasRunning, err := windowsTunnelRunning(ctx, opts.DesktopTargetPath)
	if err != nil {
		return fmt.Errorf("检查 legacy Tunnel 运行状态失败: %w", err)
	}
	layout, err := updateengine.NewWindowsLayout(opts.DesktopTargetPath)
	if err != nil {
		return err
	}

	helperPath := filepath.Join(tempRoot, "agentdock-legacy-migration-helper.exe")
	if err := copyFileWindows(opts.ExecutablePath, helperPath); err != nil {
		return fmt.Errorf("准备 Windows legacy migration helper 失败: %w", err)
	}
	plan := windowsLegacyMigrationPlan{
		ParentPID:        os.Getpid(),
		RuntimeRoot:      opts.DesktopTargetPath,
		CorePath:         layout.CoreShim(),
		TrayPath:         layout.TrayShim(),
		PayloadDir:       payloadDir,
		Version:          currentVersion,
		CoreWasRunning:   coreWasRunning,
		TrayWasRunning:   trayWasRunning,
		TunnelWasRunning: tunnelWasRunning,
		CleanupRoot:      tempRoot,
	}
	planData, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	planPath := filepath.Join(tempRoot, "migration-plan.json")
	if err := os.WriteFile(planPath, planData, 0o600); err != nil {
		return err
	}

	command := exec.Command(helperPath, windowsLegacyMigrationCommand, planPath)
	command.Dir = opts.DesktopTargetPath
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("启动 Windows legacy migration helper 失败: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return fmt.Errorf("释放 Windows legacy migration helper 失败: %w", err)
	}
	cleanup = false
	fmt.Fprintf(output, "已安排 Windows generation 一次性迁移：%s\n", currentVersion)
	return nil
}

func validateWindowsLegacyMigrationPayload(payloadDir string) error {
	for _, relative := range []string{
		"agentdock-arbiter.exe",
		"agentdock-shim.exe",
		"agentdock-tray-shim.exe",
		filepath.Join("wsl-helper", "manifest.json"),
		filepath.Join("wsl-helper", "agentdock-wsl-helper-linux-amd64"),
		filepath.Join("wsl-helper", "agentdock-wsl-helper-linux-arm64"),
	} {
		path := filepath.Join(payloadDir, relative)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("Windows legacy migration payload 缺少 %s", relative)
		}
	}
	return nil
}

func handleWindowsLegacyMigrationCommand(ctx context.Context, args []string) (bool, error) {
	if len(args) == 0 || args[0] != windowsLegacyMigrationCommand {
		return false, nil
	}
	if len(args) != 2 {
		return true, errors.New("Windows legacy migration helper 参数无效")
	}
	helperPath, err := os.Executable()
	if err != nil {
		return true, fmt.Errorf("解析 Windows legacy migration helper 路径失败: %w", err)
	}
	return true, runWindowsLegacyMigrationHelper(ctx, helperPath, args[1])
}

func runWindowsLegacyMigrationHelper(ctx context.Context, helperPath, planPath string) error {
	// 只有正在本轮 migration Temp 目录里运行的固定 helper 才能取得清理所有权。
	// 这样 plan 尚未成功解析时也可以安全回收，而不能仅凭用户可构造的目录名删目录。
	cleanupRoot, cleanupOwned := windowsLegacyMigrationCleanupRootForHelper(helperPath, planPath)
	if !cleanupOwned {
		return errors.New("Windows legacy migration helper 与 plan 不属于同一临时目录")
	}
	defer func() {
		if cleanupOwned {
			scheduleWindowsCleanup(cleanupRoot)
		}
	}()

	data, err := os.ReadFile(planPath)
	if err != nil {
		return fmt.Errorf("读取 Windows legacy migration plan 失败: %w", err)
	}
	var plan windowsLegacyMigrationPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return fmt.Errorf("解析 Windows legacy migration plan 失败: %w", err)
	}
	if !sameWindowsPath(cleanupRoot, plan.CleanupRoot) {
		return errors.New("Windows legacy migration plan 不属于当前临时目录")
	}
	if err := validateWindowsLegacyMigrationCleanupRoot(plan.CleanupRoot); err != nil {
		return err
	}

	// plan 的 cleanup root 已单独验证；从这里开始由 finalize 接管目录生命周期。
	cleanupOwned = false
	return finalizeWindowsLegacyMigration(ctx, plan)
}

func finalizeWindowsLegacyMigration(ctx context.Context, plan windowsLegacyMigrationPlan) error {
	// cleanup root 是唯一允许在 plan 完整校验前使用的字段：先证明它确实是本轮
	// 系统 Temp 子目录，再接管生命周期。这样其余 plan 字段损坏也不会泄漏 Release。
	if err := validateWindowsLegacyMigrationCleanupRoot(plan.CleanupRoot); err != nil {
		return err
	}
	preserveCleanupRoot := false
	var mutex windows.Handle
	mutexOwned := false
	defer func() {
		// cleanup 的同步 RemoveAll 必须发生在释放 migration mutex 之前。
		// 这样即使同一 plan 被异常重复启动，也不存在“先放锁、后删活跃目录”的窗口。
		if !preserveCleanupRoot {
			scheduleWindowsCleanup(plan.CleanupRoot)
		}
		if mutexOwned {
			_ = windows.ReleaseMutex(mutex)
		}
		if mutex != 0 {
			_ = windows.CloseHandle(mutex)
		}
	}()
	if err := validateWindowsLegacyMigrationPlan(plan); err != nil {
		return err
	}
	mutexName, err := windows.UTF16PtrFromString(windowsLegacyMigrationMutexName)
	if err != nil {
		return err
	}
	mutex, err = windows.CreateMutex(nil, true, mutexName)
	if err == windows.ERROR_ALREADY_EXISTS {
		if mutex == 0 {
			preserveCleanupRoot = true
			return errors.New("Windows legacy migration 互斥锁已存在但句柄不可用；保留临时目录")
		}

		// 另一 helper 可能来自独立 Temp，也可能极端情况下是同一 plan 被重复启动。
		// 只有等现有持有者释放并由本 helper 接管 mutex，才允许清理本轮目录。
		status, waitErr := windows.WaitForSingleObject(mutex, uint32((5*time.Minute)/time.Millisecond))
		if waitErr != nil {
			preserveCleanupRoot = true
			return fmt.Errorf("等待 Windows legacy migration 互斥锁失败: %w；保留临时目录", waitErr)
		}
		if status == uint32(windows.WAIT_TIMEOUT) {
			preserveCleanupRoot = true
			return errors.New("等待 Windows legacy migration 互斥锁超时；保留临时目录")
		}
		if status != windows.WAIT_OBJECT_0 && status != windows.WAIT_ABANDONED {
			preserveCleanupRoot = true
			return fmt.Errorf("等待 Windows legacy migration 互斥锁返回未知状态 %d；保留临时目录", status)
		}
		mutexOwned = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("创建 Windows legacy migration 互斥锁失败: %w", err)
	}
	mutexOwned = true

	if err := waitForWindowsProcessExit(plan.ParentPID, 30*time.Second); err != nil {
		return err
	}
	// v0.8.2/v0.8.3 的 update helper 在新 Core 健康后还会重启 Tray、同步 Skill
	// 并提交自己的 flat 更新。必须等旧 helper 完全退出再切换 stable entry，
	// 否则两套更新器会同时操作同一组文件。
	if err := waitForLegacyWindowsUpdaterExit(ctx, 30*time.Second); err != nil {
		return err
	}
	if err := validateWindowsLegacyMigrationPayload(plan.PayloadDir); err != nil {
		return err
	}

	layout, err := updateengine.NewWindowsLayout(plan.RuntimeRoot)
	if err != nil {
		return err
	}
	manifest, err := desktopruntime.LoadForBinary(plan.CorePath)
	if err != nil {
		return fmt.Errorf("读取 legacy runtime manifest 失败: %w", err)
	}

	// 先建立 committed source generation，再动稳定入口。即使此后进程被杀，
	// 下一次修复仍有一个完整 known-good generation 可供 shim 路由或继续重试。
	if _, err := installer.PrepareWindowsLegacyGeneration(ctx, installer.WindowsLegacyBootstrapRequest{
		InstallRoot: plan.RuntimeRoot,
		Version:     plan.Version,
		CorePath:    plan.CorePath,
		TrayPath:    plan.TrayPath,
		PayloadDir:  plan.PayloadDir,
	}); err != nil {
		return fmt.Errorf("建立 Windows legacy source generation 失败: %w", err)
	}

	// 备份必须发生在停服务之前：从这里开始任意一步失败，都可以直接恢复
	// flat stable entries 并按原有启动方式拉起 known-good 版本。
	backupDir := filepath.Join(plan.CleanupRoot, "stable-backup")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return err
	}
	coreBackup := filepath.Join(backupDir, "agentdock.exe")
	trayBackup := filepath.Join(backupDir, "agentdock-tray.exe")
	if err := copyFileWindows(plan.CorePath, coreBackup); err != nil {
		return fmt.Errorf("备份 legacy Core 失败: %w", err)
	}
	if err := copyFileWindows(plan.TrayPath, trayBackup); err != nil {
		return fmt.Errorf("备份 legacy Tray 失败: %w", err)
	}

	replacedCore := false
	replacedTray := false
	runtimeStopAttempted := false
	restore := func(cause error) error {
		_ = desktopruntime.StopBinaryProcesses(context.Background(), layout.GenerationCore(plan.Version), 10*time.Second)
		_ = desktopruntime.StopBinaryProcesses(context.Background(), layout.GenerationTray(plan.Version), 10*time.Second)
		var failures []string
		if replacedTray {
			if restoreErr := replaceWindowsMigrationEntry(trayBackup, plan.TrayPath); restoreErr != nil {
				failures = append(failures, "恢复 legacy Tray 失败: "+restoreErr.Error())
			}
		}
		if replacedCore {
			if restoreErr := replaceWindowsMigrationEntry(coreBackup, plan.CorePath); restoreErr != nil {
				failures = append(failures, "恢复 legacy Core 失败: "+restoreErr.Error())
			}
		}
		if runtimeStopAttempted && len(failures) == 0 {
			if restartErr := restartLegacyRuntimeAfterMigration(context.Background(), plan, manifest, false); restartErr != nil {
				failures = append(failures, restartErr.Error())
			}
		}
		if len(failures) > 0 {
			// 只有恢复不完整时保留 stable-backup，供人工或后续修复使用。
			preserveCleanupRoot = true
			return fmt.Errorf("%v；Windows legacy migration 恢复不完整: %s；保留恢复目录 %s", cause, strings.Join(failures, "；"), plan.CleanupRoot)
		}
		return fmt.Errorf("%v；已恢复 legacy 稳定入口，source generation 保留供下次重试", cause)
	}

	runtimeStopAttempted = true
	if manifest.UsesScheduledTask() {
		_ = runWindowsCommand(ctx, "schtasks.exe", "/End", "/TN", windowsScheduledTaskPath(manifest.AgentDockTaskName))
	}
	if serviceManagesTarget(ctx, plan.CorePath) {
		_ = runWindowsCommand(ctx, "sc.exe", "stop", windowsServiceName)
		_ = waitWindowsServiceState(ctx, windowsServiceName, "STOPPED", 20*time.Second)
	}
	if err := desktopruntime.StopBinaryProcesses(ctx, plan.CorePath, 15*time.Second); err != nil {
		return restore(fmt.Errorf("停止 legacy Core 失败: %w", err))
	}
	if err := desktopruntime.StopBinaryProcesses(ctx, plan.TrayPath, 15*time.Second); err != nil {
		return restore(fmt.Errorf("停止 legacy Tray 失败: %w", err))
	}

	if err := replaceWindowsMigrationEntry(filepath.Join(plan.PayloadDir, "agentdock-shim.exe"), plan.CorePath); err != nil {
		return restore(fmt.Errorf("安装稳定 Core shim 失败: %w", err))
	}
	replacedCore = true
	if err := replaceWindowsMigrationEntry(filepath.Join(plan.PayloadDir, "agentdock-tray-shim.exe"), plan.TrayPath); err != nil {
		return restore(fmt.Errorf("安装稳定 Tray shim 失败: %w", err))
	}
	replacedTray = true
	if err := verifyBinaryVersion(ctx, plan.CorePath, plan.Version); err != nil {
		return restore(fmt.Errorf("稳定 Core shim 路由验证失败: %w", err))
	}
	if err := restartLegacyRuntimeAfterMigration(ctx, plan, manifest, true); err != nil {
		return restore(err)
	}

	// 旧 updater 只把这个脚本当作 Release 契约和过渡期运行文件；迁移成功后当前
	// runtime 已全部走原生实现，删除它避免兼容资产重新变成长期运行入口。
	compatManager := filepath.Join(plan.RuntimeRoot, "installer", "manage-windows.ps1")
	_ = os.Remove(compatManager)
	_ = os.Remove(filepath.Dir(compatManager))
	return nil
}

func waitForLegacyWindowsUpdaterExit(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		running, err := windowsProcessNameRunning("agentdock-update-helper.exe")
		if err != nil {
			return fmt.Errorf("检查旧 Windows updater 状态失败: %w", err)
		}
		if !running {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("等待旧 Windows updater 完成超时")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func windowsProcessNameRunning(name string) (bool, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(snapshot)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return false, nil
		}
		return false, err
	}
	for {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), name) {
			return true, nil
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				return false, nil
			}
			return false, err
		}
	}
}

func windowsLegacyMigrationCleanupRootForHelper(helperPath, planPath string) (string, bool) {
	if !strings.EqualFold(filepath.Base(helperPath), "agentdock-legacy-migration-helper.exe") ||
		!strings.EqualFold(filepath.Base(planPath), "migration-plan.json") {
		return "", false
	}
	helperRoot := filepath.Clean(filepath.Dir(helperPath))
	planRoot := filepath.Clean(filepath.Dir(planPath))
	if !sameWindowsPath(helperRoot, planRoot) {
		return "", false
	}
	if err := validateWindowsLegacyMigrationCleanupRoot(helperRoot); err != nil {
		return "", false
	}
	return helperRoot, true
}

func validateWindowsLegacyMigrationCleanupRoot(root string) error {
	root = filepath.Clean(strings.TrimSpace(root))
	tempRoot := filepath.Clean(os.TempDir())
	relative, err := filepath.Rel(tempRoot, root)
	if err != nil ||
		relative == "." ||
		relative == ".." ||
		filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, ".."+string(os.PathSeparator)) ||
		!strings.HasPrefix(filepath.Base(root), "agentdock-legacy-migration-") {
		return errors.New("Windows legacy migration cleanup root 必须位于系统 Temp")
	}
	return nil
}

func validateWindowsLegacyMigrationPlan(plan windowsLegacyMigrationPlan) error {
	if plan.ParentPID <= 0 ||
		strings.TrimSpace(plan.RuntimeRoot) == "" ||
		strings.TrimSpace(plan.CorePath) == "" ||
		strings.TrimSpace(plan.TrayPath) == "" ||
		strings.TrimSpace(plan.PayloadDir) == "" ||
		strings.TrimSpace(plan.CleanupRoot) == "" {
		return errors.New("Windows legacy migration plan 缺少必要字段")
	}
	if err := updateengine.ValidateVersion(plan.Version); err != nil {
		return fmt.Errorf("Windows legacy migration version 无效: %w", err)
	}
	if err := validateWindowsLegacyMigrationCleanupRoot(plan.CleanupRoot); err != nil {
		return err
	}
	layout, err := updateengine.NewWindowsLayout(plan.RuntimeRoot)
	if err != nil {
		return err
	}
	if !sameWindowsPath(plan.CorePath, layout.CoreShim()) || !sameWindowsPath(plan.TrayPath, layout.TrayShim()) {
		return errors.New("Windows legacy migration 只允许受管 stable Core/Tray 路径")
	}
	return nil
}

func replaceWindowsMigrationEntry(sourcePath, targetPath string) error {
	info, err := os.Stat(sourcePath)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("迁移源文件不可用: %s", sourcePath)
	}
	newPath := targetPath + ".migration-new"
	_ = os.Remove(newPath)
	if err := copyFileWindows(sourcePath, newPath); err != nil {
		return err
	}
	if err := moveFileReplace(newPath, targetPath); err != nil {
		_ = os.Remove(newPath)
		return err
	}
	return nil
}

func restartLegacyRuntimeAfterMigration(
	ctx context.Context,
	plan windowsLegacyMigrationPlan,
	manifest desktopruntime.Manifest,
	migrated bool,
) error {
	corePath := plan.CorePath
	trayPath := plan.TrayPath
	if plan.CoreWasRunning {
		if healthy := waitForVersion(ctx, []string{manifest.HealthURL()}, plan.Version, 2*time.Second); healthy != nil {
			command := exec.CommandContext(ctx, corePath, "service", "start", "--runtime-root", plan.RuntimeRoot)
			command.Dir = plan.RuntimeRoot
			if output, err := command.CombinedOutput(); err != nil {
				return fmt.Errorf("重新启动 Windows Core 失败: %w: %s", err, strings.TrimSpace(string(output)))
			}
		}
		if err := waitForVersion(ctx, []string{manifest.HealthURL()}, plan.Version, 45*time.Second); err != nil {
			return fmt.Errorf("Windows migration 健康检查失败: %w", err)
		}
	}
	if plan.TrayWasRunning {
		running, err := desktopruntime.BinaryProcessRunning(trayPath)
		if err != nil {
			return fmt.Errorf("检查 Windows Tray 恢复状态失败: %w", err)
		}
		if !running {
			command := exec.Command(trayPath, "--background")
			command.Dir = plan.RuntimeRoot
			command.SysProcAttr = &syscall.SysProcAttr{
				HideWindow:    true,
				CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
			}
			if err := command.Start(); err != nil {
				return fmt.Errorf("重新启动 Windows Tray 失败: %w", err)
			}
			_ = command.Process.Release()
		}
	}
	if plan.TunnelWasRunning {
		command := exec.Command(trayPath, "--start-tunnel", "--runtime-root", plan.RuntimeRoot)
		command.Dir = plan.RuntimeRoot
		command.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
		}
		if err := command.Start(); err != nil {
			return fmt.Errorf("恢复 Windows Tunnel 失败: %w", err)
		}
		_ = command.Process.Release()
	}
	if migrated {
		layout, err := updateengine.NewWindowsLayout(plan.RuntimeRoot)
		if err != nil {
			return err
		}
		if info, err := os.Stat(layout.GenerationCore(plan.Version)); err != nil || !info.Mode().IsRegular() {
			return errors.New("Windows generation Core 在迁移后不可用")
		}
	}
	return nil
}
