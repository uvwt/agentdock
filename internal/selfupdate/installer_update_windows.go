//go:build windows

package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/uvwt/agentdock/internal/authenticode"
)

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

	// 完整 Setup 是 Windows 安装状态的唯一写入入口。这里不再拆 Release ZIP，
	// 也不再让 updater 理解 generation、WinUI 伴随文件或运行时依赖。
	command := exec.Command(
		installerPath,
		"/SILENT",
		"/SUPPRESSMSGBOXES",
		"/NORESTART",
		"/CLOSEAPPLICATIONS",
		"/DIR="+installRoot,
	)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := command.Start(); err != nil {
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
