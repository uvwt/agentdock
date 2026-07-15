//go:build darwin

package selfupdate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const macOSLaunchAgentLabel = "com.uvwt.agentdock"

type launchdService struct {
	domain string
	label  string
}

func (service launchdService) Name() string {
	return "LaunchAgent " + service.label
}

func (service launchdService) Restart(ctx context.Context) error {
	output, err := exec.CommandContext(ctx, "launchctl", "kickstart", "-k", service.domain+"/"+service.label).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl kickstart 失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func detectManagedService(ctx context.Context, targetPath string) managedService {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	// 当前官方 macOS 安装器只安装 CLI；只有已存在的裸机 LaunchAgent 运行路径才自动重启。
	if filepath.Clean(targetPath) != filepath.Join(home, "agentdock", "agentdock") {
		return nil
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	if err := exec.CommandContext(ctx, "launchctl", "print", domain+"/"+macOSLaunchAgentLabel).Run(); err != nil {
		return nil
	}
	return launchdService{domain: domain, label: macOSLaunchAgentLabel}
}

func platformHealthCandidates(context.Context, string) []string {
	return nil
}

func signLocalReplacement(ctx context.Context, targetPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	if filepath.Clean(targetPath) != filepath.Join(home, "agentdock", "agentdock") {
		return nil
	}
	script := filepath.Join(home, "agentdock-runtime", "sign-agentdock.sh")
	info, err := os.Stat(script)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("本地签名脚本不是普通文件: %s", script)
	}
	output, err := exec.CommandContext(ctx, script, targetPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
