//go:build windows

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
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/uvwt/agentdock/internal/authenticode"
	"github.com/uvwt/agentdock/internal/fs/atomicfile"
)

const (
	windowsUpdateUIHandoffEnv      = "AGENTDOCK_UPDATE_UI_HANDOFF"
	windowsInstallerHandoffFile    = "installer-handoff.json"
	windowsInstallerHandoffVersion = 1
)

type windowsInstallerHandoffMarker struct {
	SchemaVersion int    `json:"schema_version"`
	SourceVersion string `json:"source_version"`
	TargetVersion string `json:"target_version"`
	CreatedAt     string `json:"created_at"`
}

func windowsInstallerArgs(installRoot string, interactiveUI bool) []string {
	// 保留 /SILENT 以在旧版桌面端退出后展示原生安装进度条，避免用户误以为闪退；
	// 仅在非 UI 无人值守场景追加 /SUPPRESSMSGBOXES，防止交互式更新失败时静默吞掉错误弹窗。
	args := []string{
		"/SILENT",
		"/NORESTART",
		"/CLOSEAPPLICATIONS",
		"/DIR=" + installRoot,
	}
	if !interactiveUI {
		args = append(args, "/SUPPRESSMSGBOXES")
	}
	return args
}

func writeWindowsInstallerHandoffMarker(installRoot, currentVersion, targetVersion string, now time.Time) (string, error) {
	markerPath := filepath.Join(installRoot, "update", windowsInstallerHandoffFile)
	payload, err := json.Marshal(windowsInstallerHandoffMarker{
		SchemaVersion: windowsInstallerHandoffVersion,
		SourceVersion: normalizeVersion(currentVersion),
		TargetVersion: normalizeVersion(targetVersion),
		CreatedAt:     now.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return "", err
	}
	if err := atomicfile.Write(markerPath, append(payload, '\n'), 0o600); err != nil {
		return "", err
	}
	return markerPath, nil
}

func applyPlatformInstallerUpdate(ctx context.Context, request installerApplyRequest) (applyResult, error) {
	if err := ctx.Err(); err != nil {
		return applyResult{}, err
	}
	installRoot := filepath.Clean(strings.TrimSpace(request.InstallRoot))
	if installRoot == "." || !filepath.IsAbs(installRoot) {
		return applyResult{}, errors.New("Windows 完整安装器更新缺少有效安装目录")
	}
	if len(request.InstallerData) == 0 {
		return applyResult{}, errors.New("Windows 完整安装器为空")
	}
	installerName := filepath.Base(strings.TrimSpace(request.InstallerName))
	if installerName == "." || !strings.EqualFold(filepath.Ext(installerName), ".exe") {
		return applyResult{}, fmt.Errorf("Windows 完整安装器文件名无效: %q", request.InstallerName)
	}

	stagingRoot, err := os.MkdirTemp("", "agentdock-installer-update-*")
	if err != nil {
		return applyResult{}, fmt.Errorf("创建 Windows 安装器暂存目录失败: %w", err)
	}
	installerPath := filepath.Join(stagingRoot, installerName)
	if err := os.WriteFile(installerPath, request.InstallerData, 0o700); err != nil {
		_ = os.RemoveAll(stagingRoot)
		return applyResult{}, fmt.Errorf("写入 Windows 完整安装器失败: %w", err)
	}
	if err := authenticode.VerifyFileOrSameSigner(ctx, installerPath, request.CurrentPath); err != nil {
		_ = os.RemoveAll(stagingRoot)
		return applyResult{}, fmt.Errorf("Windows 完整安装器签名验证失败: %w", err)
	}

	interactiveUI := strings.TrimSpace(os.Getenv(windowsUpdateUIHandoffEnv)) == "1"
	markerPath := ""
	if interactiveUI {
		// 标记由桌面端发起的完整安装器交接，供升级重启后的新版托盘给出最终完成提示。
		// 写入失败不影响安装器启动本身。
		if path, err := writeWindowsInstallerHandoffMarker(installRoot, request.CurrentVersion, request.TargetVersion, time.Now()); err == nil {
			markerPath = path
		}
	}

	// 完整 Setup 是 Windows 安装状态的唯一写入入口。这里不再拆 Release ZIP，
	// 也不再让 updater 理解 generation、WinUI 伴随文件或运行时依赖。
	command := exec.Command(installerPath, windowsInstallerArgs(installRoot, interactiveUI)...)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := command.Start(); err != nil {
		if markerPath != "" {
			_ = os.Remove(markerPath)
		}
		_ = os.RemoveAll(stagingRoot)
		return applyResult{}, fmt.Errorf("启动 Windows 完整安装器失败: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return applyResult{}, fmt.Errorf("释放 Windows 安装器进程句柄失败: %w", err)
	}

	// Setup 已经成为独立进程；不要删除其所在 TEMP 目录。系统 TEMP 生命周期负责
	// 最终回收，避免 updater 退出时抢先删除仍在运行的安装器。
	return applyResult{HandedOff: true}, nil
}
