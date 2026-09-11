//go:build windows

package selfupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/desktopruntime"
)

const (
	windowsDesktopVersionFile  = "desktop-version.txt"
	maxWindowsDesktopFileBytes = 256 << 20
)

var windowsDesktopArchiveFiles = map[string]os.FileMode{
	"agentdock-tray.exe": 0o755,
	"agentdock.ico":      0o644,
	"manage-windows.ps1": 0o644,
}

// 新 generation 架构在同一 Release ZIP 中附带稳定入口和 Arbiter。
// 这些文件对旧 0.8.x updater 保持可选，因此旧版本仍能消费同一 Release；
// generation-aware updater 会在真正进入 trial 前强制校验它们存在。
var windowsGenerationArchiveFiles = map[string]os.FileMode{
	"agentdock-arbiter.exe":                       0o755,
	"agentdock-shim.exe":                          0o755,
	"agentdock-tray-shim.exe":                     0o755,
	"wsl-helper/manifest.json":                    0o644,
	"wsl-helper/agentdock-wsl-helper-linux-amd64": 0o755,
	"wsl-helper/agentdock-wsl-helper-linux-arm64": 0o755,
}

func detectDesktopUpdateTarget() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	manifest, err := desktopruntime.LoadForBinary(executable)
	if err != nil || strings.TrimSpace(manifest.TrayBinary) == "" {
		return ""
	}
	runtimeRoot := strings.TrimSpace(manifest.InstallRoot)
	if runtimeRoot == "" {
		runtimeRoot = filepath.Dir(filepath.Dir(executable))
	}
	if !filepath.IsAbs(runtimeRoot) {
		return ""
	}
	return filepath.Clean(runtimeRoot)
}

func desktopUpdateVersion(runtimeRoot string) string {
	if strings.TrimSpace(runtimeRoot) == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(runtimeRoot, windowsDesktopVersionFile))
	if err != nil {
		return ""
	}
	return normalizeVersion(strings.TrimSpace(string(data)))
}

func desktopUpdateOwnsExecutable(string, string) bool {
	return false
}

func extractDesktopUpdateArchive(_ context.Context, archiveData []byte, tempDir, targetVersion string) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
	if err != nil {
		return "", err
	}

	stagedRoot := filepath.Join(tempDir, "windows-desktop")
	if err := os.MkdirAll(stagedRoot, 0o700); err != nil {
		return "", fmt.Errorf("创建 Windows 桌面组件暂存目录失败: %w", err)
	}

	found := make(map[string]bool, len(windowsDesktopArchiveFiles)+len(windowsGenerationArchiveFiles))
	for _, file := range reader.File {
		name := filepath.ToSlash(file.Name)
		mode, wanted := windowsDesktopArchiveFiles[name]
		if !wanted {
			mode, wanted = windowsGenerationArchiveFiles[name]
		}
		if !wanted {
			continue
		}
		if found[name] {
			return "", fmt.Errorf("Windows Release ZIP 包含重复文件 %s", name)
		}
		if !file.Mode().IsRegular() || file.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("Windows Release ZIP 中 %s 不是普通文件", name)
		}
		opened, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("读取 Windows Release 文件 %s 失败: %w", name, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(opened, maxWindowsDesktopFileBytes+1))
		closeErr := opened.Close()
		if readErr != nil {
			return "", fmt.Errorf("读取 Windows Release 文件 %s 失败: %w", name, readErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("关闭 Windows Release 文件 %s 失败: %w", name, closeErr)
		}
		if len(data) == 0 {
			return "", fmt.Errorf("Windows Release 文件 %s 为空", name)
		}
		if len(data) > maxWindowsDesktopFileBytes {
			return "", fmt.Errorf("Windows Release 文件 %s 超过 %d 字节限制", name, maxWindowsDesktopFileBytes)
		}
		destination := filepath.Join(stagedRoot, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return "", fmt.Errorf("创建 Windows 桌面组件目录 %s 失败: %w", name, err)
		}
		if err := os.WriteFile(destination, data, mode); err != nil {
			return "", fmt.Errorf("写入 Windows 桌面组件 %s 失败: %w", name, err)
		}
		found[name] = true
	}
	for name := range windowsDesktopArchiveFiles {
		if !found[name] {
			return "", fmt.Errorf("Windows Release ZIP 缺少 %s", name)
		}
	}

	version := normalizeVersion(targetVersion)
	if version == "" {
		return "", errors.New("Windows 桌面组件目标版本为空")
	}
	if err := os.WriteFile(filepath.Join(stagedRoot, windowsDesktopVersionFile), []byte(version+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("写入 Windows 桌面组件版本标记失败: %w", err)
	}
	return stagedRoot, nil
}
