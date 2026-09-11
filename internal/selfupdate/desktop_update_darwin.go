//go:build darwin

package selfupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxExtractedDesktopBytes = 1 << 30

func detectDesktopUpdateTarget() string {
	candidates := make([]string, 0, 3)
	if configured := strings.TrimSpace(os.Getenv("AGENTDOCK_DESKTOP_APP_PATH")); configured != "" {
		candidates = append(candidates, configured)
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			"/Applications/AgentDock.app",
			filepath.Join(home, "Applications", "AgentDock.app"),
		)
	}
	for _, candidate := range uniqueStrings(candidates) {
		candidate = filepath.Clean(candidate)
		if validateMacOSDesktopTarget(candidate) == nil {
			return candidate
		}
	}
	return ""
}

func desktopUpdateVersion(appPath string) string {
	if strings.TrimSpace(appPath) == "" {
		return ""
	}
	version, err := plistValue(context.Background(), filepath.Join(appPath, "Contents", "Info.plist"), "CFBundleShortVersionString")
	if err != nil {
		return ""
	}
	normalized := normalizeVersion(version)
	if _, valid := compareVersions(normalized, normalized); !valid {
		return ""
	}
	return normalized
}

func desktopUpdateOwnsExecutable(appPath, executable string) bool {
	if strings.TrimSpace(appPath) == "" || strings.TrimSpace(executable) == "" {
		return false
	}
	expected := filepath.Join(filepath.Clean(appPath), "Contents", "Helpers", "agentdock")
	if resolved, err := filepath.EvalSymlinks(expected); err == nil {
		expected = resolved
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	return filepath.Clean(expected) == filepath.Clean(executable)
}

func extractDesktopUpdateArchive(ctx context.Context, archiveData []byte, tempDir, targetVersion string) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
	if err != nil {
		return "", err
	}
	root := filepath.Join(tempDir, "macos-desktop")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("创建桌面更新暂存目录失败: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return "", fmt.Errorf("设置桌面更新暂存目录权限失败: %w", err)
	}

	var extracted uint64
	for _, file := range reader.File {
		name := strings.TrimPrefix(filepath.ToSlash(file.Name), "./")
		if strings.HasPrefix(name, "__MACOSX/") || name == ".DS_Store" {
			continue
		}
		clean := filepath.ToSlash(filepath.Clean(name))
		if clean == "." || strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
			return "", fmt.Errorf("桌面更新 ZIP 包含越界路径 %q", file.Name)
		}
		if containsAppleDoubleComponent(clean) {
			continue
		}
		if clean != "AgentDock.app" && !strings.HasPrefix(clean, "AgentDock.app/") {
			return "", fmt.Errorf("桌面更新 ZIP 包含非 App 内容 %q", file.Name)
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("桌面更新 ZIP 不允许符号链接 %q", file.Name)
		}
		if file.UncompressedSize64 > maxExtractedDesktopBytes-extracted {
			return "", fmt.Errorf("解压后的 macOS App 超过 %d 字节限制", maxExtractedDesktopBytes)
		}
		extracted += file.UncompressedSize64

		target := filepath.Join(root, filepath.FromSlash(clean))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", fmt.Errorf("创建 App 目录失败: %w", err)
			}
			continue
		}
		if !file.Mode().IsRegular() {
			return "", fmt.Errorf("桌面更新 ZIP 包含不支持的文件类型 %q", file.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", fmt.Errorf("创建 App 父目录失败: %w", err)
		}
		opened, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("打开 ZIP 条目失败: %w", err)
		}
		mode := file.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		created, createErr := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if createErr != nil {
			_ = opened.Close()
			return "", fmt.Errorf("创建 App 文件失败: %w", createErr)
		}
		_, copyErr := io.Copy(created, io.LimitReader(opened, int64(file.UncompressedSize64)+1))
		closeErr := created.Close()
		_ = opened.Close()
		if copyErr != nil {
			return "", fmt.Errorf("写入 App 文件失败: %w", copyErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("关闭 App 文件失败: %w", closeErr)
		}
		if err := os.Chmod(target, mode); err != nil {
			return "", fmt.Errorf("设置 App 文件权限失败: %w", err)
		}
	}

	appPath := filepath.Join(root, "AgentDock.app")
	if err := validateMacOSDesktopRuntime(ctx, appPath, targetVersion); err != nil {
		return "", err
	}
	return appPath, nil
}

func containsAppleDoubleComponent(path string) bool {
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.HasPrefix(component, "._") {
			return true
		}
	}
	return false
}

func validateMacOSDesktopTarget(appPath string) error {
	info, err := os.Lstat(appPath)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("macOS App 不是普通目录: %s", appPath)
	}
	identifier, err := plistValue(context.Background(), filepath.Join(appPath, "Contents", "Info.plist"), "CFBundleIdentifier")
	if err != nil || identifier != "com.uvwt.agentdock" {
		return fmt.Errorf("macOS App Bundle Identifier 无效: %s", appPath)
	}
	executable := filepath.Join(appPath, "Contents", "MacOS", "AgentDock")
	if !executableRegularFile(executable) {
		return fmt.Errorf("macOS App 缺少可执行文件: %s", executable)
	}
	return nil
}

func validateMacOSDesktopVersion(ctx context.Context, appPath, targetVersion string) error {
	if err := validateMacOSDesktopTarget(appPath); err != nil {
		return err
	}
	version, err := plistValue(ctx, filepath.Join(appPath, "Contents", "Info.plist"), "CFBundleShortVersionString")
	if err != nil {
		return fmt.Errorf("读取 macOS App 版本失败: %w", err)
	}
	if normalizeVersion(version) != normalizeVersion(targetVersion) {
		return fmt.Errorf("macOS App 版本为 %s，目标版本为 %s", normalizeVersion(version), normalizeVersion(targetVersion))
	}
	output, err := exec.CommandContext(ctx, "codesign", "--verify", "--deep", "--strict", "--verbose=2", appPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("macOS App 代码签名验证失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func validateMacOSDesktopRuntime(ctx context.Context, appPath, targetVersion string) error {
	if err := validateMacOSDesktopVersion(ctx, appPath, targetVersion); err != nil {
		return err
	}
	core := filepath.Join(appPath, "Contents", "Helpers", "agentdock")
	cloudflared := filepath.Join(appPath, "Contents", "Helpers", "cloudflared")
	arbiter := filepath.Join(appPath, "Contents", "Helpers", "agentdock-arbiter")
	menuHelper := filepath.Join(appPath, "Contents", "Helpers", "AgentDockLoginHelper")
	menuAgent := filepath.Join(appPath, "Contents", "Library", "LaunchAgents", "com.uvwt.agentdock.menu-login.plist")
	skillManifest := filepath.Join(appPath, "Contents", "Resources", "core-skills", "manifest.json")
	for _, path := range []string{core, cloudflared, arbiter, menuHelper, menuAgent, skillManifest} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("macOS App 缺少有效运行组件: %s", path)
		}
	}
	if !executableRegularFile(core) || !executableRegularFile(cloudflared) || !executableRegularFile(arbiter) || !executableRegularFile(menuHelper) {
		return errors.New("macOS App 内置 Core、cloudflared、Arbiter 或菜单栏登录组件不可执行")
	}
	for _, expected := range []struct {
		key   string
		value string
	}{
		{key: "Label", value: "com.uvwt.agentdock.menu-login"},
		{key: "BundleProgram", value: "Contents/Helpers/AgentDockLoginHelper"},
		{key: "ProgramArguments.0", value: "AgentDockLoginHelper"},
		{key: "RunAtLoad", value: "true"},
		{key: "LimitLoadToSessionType", value: "Aqua"},
	} {
		value, err := plistValue(ctx, menuAgent, expected.key)
		if err != nil || value != expected.value {
			return fmt.Errorf("macOS App 菜单栏登录服务 %s 无效", expected.key)
		}
	}
	if _, err := plistValue(ctx, menuAgent, "ProgramArguments.1"); err == nil {
		return errors.New("macOS App 菜单栏登录服务 ProgramArguments 只能包含 AgentDockLoginHelper")
	}
	for _, forbiddenKey := range []string{"KeepAlive", "ProcessType"} {
		if _, err := plistValue(ctx, menuAgent, forbiddenKey); err == nil {
			return fmt.Errorf("macOS App 菜单栏登录服务不应包含 %s", forbiddenKey)
		}
	}
	if err := verifyBinaryVersion(ctx, core, targetVersion); err != nil {
		return fmt.Errorf("macOS App 内置 Core 版本不匹配: %w", err)
	}
	if output, err := exec.CommandContext(ctx, "codesign", "--verify", "--strict", "--verbose=2", arbiter).CombinedOutput(); err != nil {
		return fmt.Errorf("macOS App 内置 Arbiter 签名验证失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if output, err := exec.CommandContext(ctx, cloudflared, "--version").CombinedOutput(); err != nil {
		return fmt.Errorf("macOS App 内置 cloudflared 无法运行: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func plistValue(ctx context.Context, plistPath, key string) (string, error) {
	info, err := os.Lstat(plistPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("plist 不是普通文件: %s", plistPath)
	}
	output, err := exec.CommandContext(ctx, "plutil", "-extract", key, "raw", "-o", "-", plistPath).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("读取 plist %s 失败: %w: %s", key, err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
