//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/installer"
)

func runInstallPrepareWindowsLegacy(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock install prepare-windows-legacy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var request installer.WindowsLegacyBootstrapRequest
	flags.StringVar(&request.InstallRoot, "install-root", "", "AgentDock Windows 安装根目录")
	flags.StringVar(&request.Version, "legacy-version", "", "当前 legacy 安装版本")
	flags.StringVar(&request.CorePath, "legacy-core", "", "当前 legacy Core 路径")
	flags.StringVar(&request.TrayPath, "legacy-tray", "", "当前 legacy Tray 路径")
	flags.StringVar(&request.PayloadDir, "payload-dir", "", "包含 Arbiter/WSL helper 的新 Release 载荷目录")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("未知参数：%s", flags.Arg(0))
	}
	result, err := installer.PrepareWindowsLegacyGeneration(ctx, request)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(result)
}

func runInstallDetachEngine(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("agentdock install detach-engine", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "", "卸载事务使用的临时 Installer Engine 路径")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("未知参数：%s", flags.Arg(0))
	}
	destination := strings.TrimSpace(*output)
	if destination == "" {
		return fmt.Errorf("output 不能为空")
	}
	source, err := os.Executable()
	if err != nil {
		return fmt.Errorf("读取当前 Installer Engine 路径：%w", err)
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("解析当前 Installer Engine 路径：%w", err)
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("解析 detached Engine 路径：%w", err)
	}
	if strings.EqualFold(filepath.Clean(source), filepath.Clean(destination)) {
		return fmt.Errorf("detached Engine 不能覆盖当前可执行文件")
	}
	if err := copyDetachedEngine(source, destination); err != nil {
		return err
	}

	verifyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(verifyCtx, destination, "install", "--engine-ready")
	var ready bytes.Buffer
	command.Stdout = &ready
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		_ = os.Remove(destination)
		return fmt.Errorf("验证 detached Installer Engine：%w", err)
	}
	if strings.TrimSpace(ready.String()) != "agentdock-installer-engine" {
		_ = os.Remove(destination)
		return fmt.Errorf("detached Installer Engine 握手无效")
	}
	fmt.Fprintln(stdout, "agentdock-installer-engine-helper")
	return nil
}

func copyDetachedEngine(source, destination string) (returnErr error) {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("打开当前 Installer Engine：%w", err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return fmt.Errorf("读取当前 Installer Engine 元数据：%w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("创建 detached Engine 目录：%w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".agentdock-engine-*.tmp")
	if err != nil {
		return fmt.Errorf("创建 detached Engine 临时文件：%w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if returnErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		return fmt.Errorf("设置 detached Engine 权限：%w", err)
	}
	if _, err := io.Copy(temporary, input); err != nil {
		return fmt.Errorf("复制 detached Installer Engine：%w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("同步 detached Installer Engine：%w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭 detached Installer Engine：%w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return fmt.Errorf("发布 detached Installer Engine：%w", err)
	}
	return nil
}
