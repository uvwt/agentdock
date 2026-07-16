//go:build darwin

package selfupdate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const macOSLaunchAgentLabel = "com.uvwt.agentdock"

type macOSPaths struct {
	binary      string
	serviceEnv  string
	startScript string
	launchAgent string
	workDir     string
	backupDir   string
	stdoutLog   string
	stderrLog   string
}

type launchdService struct {
	domain string
	label  string
	port   int
}

func standardMacOSPaths(home string) macOSPaths {
	appSupport := filepath.Join(home, "Library", "Application Support", "AgentDock")
	logDir := filepath.Join(home, "Library", "Logs", "AgentDock")
	return macOSPaths{
		binary:      filepath.Join(home, ".local", "bin", "agentdock"),
		serviceEnv:  filepath.Join(appSupport, "agentdock.env"),
		startScript: filepath.Join(appSupport, "start-agentdock.sh"),
		launchAgent: filepath.Join(home, "Library", "LaunchAgents", macOSLaunchAgentLabel+".plist"),
		workDir:     filepath.Join(home, "AgentDock"),
		backupDir:   filepath.Join(home, ".agentdock", "backups", "bin"),
		stdoutLog:   filepath.Join(logDir, "agentdock.out.log"),
		stderrLog:   filepath.Join(logDir, "agentdock.err.log"),
	}
}

func macOSServiceAddress(path string) (string, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0
	}
	portMatch := regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?AGENTDOCK_PORT[ \t]*=[ \t]*['"]?([0-9]+)['"]?[ \t]*(?:#.*)?$`).FindStringSubmatch(string(data))
	if len(portMatch) != 2 {
		return "", 0
	}
	port, err := strconv.Atoi(portMatch[1])
	if err != nil || port <= 0 || port > 65535 {
		return "", 0
	}

	host := "127.0.0.1"
	hostMatch := regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?AGENTDOCK_HOST[ \t]*=[ \t]*['"]?([^'"#[:space:]]+)['"]?[ \t]*(?:#.*)?$`).FindStringSubmatch(string(data))
	if len(hostMatch) == 2 {
		host = strings.TrimSpace(hostMatch[1])
	}
	return host, port
}

func macOSServicePort(path string) int {
	_, port := macOSServiceAddress(path)
	return port
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

func (service launchdService) CurrentPID(ctx context.Context) (int, error) {
	output, err := exec.CommandContext(ctx, "launchctl", "print", service.domain+"/"+service.label).Output()
	if err != nil {
		return 0, fmt.Errorf("读取 LaunchAgent 状态失败: %w", err)
	}
	pid := parseLaunchdPID(string(output))
	if pid <= 0 {
		return 0, fmt.Errorf("LaunchAgent %s 没有运行中的 PID", service.label)
	}
	return pid, nil
}

func (service launchdService) WaitForRestart(ctx context.Context, targetPath string, previousPID int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastError error
	for time.Now().Before(deadline) {
		pid, err := service.CurrentPID(ctx)
		if err != nil {
			lastError = err
		} else if pid == previousPID {
			lastError = fmt.Errorf("LaunchAgent 仍在使用旧 PID %d", pid)
		} else if err := verifyMacOSProcess(ctx, pid, targetPath, service.port); err != nil {
			lastError = err
		} else {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if lastError == nil {
		lastError = fmt.Errorf("未观察到新的 LaunchAgent 进程")
	}
	return lastError
}

func parseLaunchdPID(output string) int {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "pid = ") {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "pid = ")))
		if err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

func verifyMacOSProcess(ctx context.Context, pid int, targetPath string, port int) error {
	commandOutput, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return fmt.Errorf("读取 PID %d 命令失败: %w", pid, err)
	}
	commandLine := strings.TrimSpace(string(commandOutput))
	cleanTarget := filepath.Clean(targetPath)
	if commandLine != cleanTarget && !strings.HasPrefix(commandLine, cleanTarget+" ") {
		return fmt.Errorf("PID %d 未运行目标二进制 %s", pid, targetPath)
	}

	lsofOutput, err := exec.CommandContext(
		ctx,
		"lsof",
		"-nP",
		"-a",
		"-p", strconv.Itoa(pid),
		"-iTCP:"+strconv.Itoa(port),
		"-sTCP:LISTEN",
		"-t",
	).Output()
	if err != nil || !containsPID(string(lsofOutput), pid) {
		return fmt.Errorf("PID %d 未监听目标端口 %d", pid, port)
	}
	return nil
}

func containsPID(output string, pid int) bool {
	want := strconv.Itoa(pid)
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func detectManagedService(ctx context.Context, targetPath string) managedService {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	paths := standardMacOSPaths(home)
	if filepath.Clean(targetPath) != paths.binary {
		return nil
	}
	if !regularFile(paths.launchAgent) || !executableRegularFile(paths.startScript) || !regularFile(paths.serviceEnv) {
		return nil
	}
	if err := exec.CommandContext(ctx, "plutil", "-lint", paths.launchAgent).Run(); err != nil {
		return nil
	}
	plistPaths := map[string]string{
		"ProgramArguments.0": paths.startScript,
		"WorkingDirectory":   paths.workDir,
		"StandardOutPath":    paths.stdoutLog,
		"StandardErrorPath":  paths.stderrLog,
	}
	for key, expected := range plistPaths {
		value, valueErr := readPlistString(ctx, paths.launchAgent, key)
		if valueErr != nil || filepath.Clean(value) != expected {
			return nil
		}
	}

	domain := "gui/" + strconv.Itoa(os.Getuid())
	if err := exec.CommandContext(ctx, "launchctl", "print", domain+"/"+macOSLaunchAgentLabel).Run(); err != nil {
		return nil
	}
	port := macOSServicePort(paths.serviceEnv)
	if port == 0 {
		return nil
	}
	return launchdService{domain: domain, label: macOSLaunchAgentLabel, port: port}
}

func readPlistString(ctx context.Context, plistPath, key string) (string, error) {
	output, err := exec.CommandContext(ctx, "plutil", "-extract", key, "raw", "-o", "-", plistPath).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func platformHealthCandidates(_ context.Context, targetPath string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	paths := standardMacOSPaths(home)
	if filepath.Clean(targetPath) != paths.binary {
		return nil
	}
	host, port := macOSServiceAddress(paths.serviceEnv)
	if port == 0 {
		return nil
	}
	return []string{localHealthURL(host, port)}
}

func platformBackupPath(targetPath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("解析 macOS 用户目录失败: %w", err)
	}
	paths := standardMacOSPaths(home)
	if filepath.Clean(targetPath) != paths.binary {
		return targetPath + ".backup", nil
	}
	if err := os.MkdirAll(paths.backupDir, 0o700); err != nil {
		return "", fmt.Errorf("创建 macOS 更新备份目录失败: %w", err)
	}
	if err := os.Chmod(paths.backupDir, 0o700); err != nil {
		return "", fmt.Errorf("设置 macOS 更新备份目录权限失败: %w", err)
	}
	name := "agentdock." + time.Now().UTC().Format("20060102150405.000000000")
	return filepath.Join(paths.backupDir, name), nil
}

func signLocalReplacement(ctx context.Context, targetPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	paths := standardMacOSPaths(home)
	if filepath.Clean(targetPath) != paths.binary {
		return nil
	}

	identity := strings.TrimSpace(os.Getenv("AGENTDOCK_CODESIGN_IDENTITY"))
	keychain := strings.TrimSpace(os.Getenv("AGENTDOCK_CODESIGN_KEYCHAIN"))
	keychainPassword := os.Getenv("AGENTDOCK_CODESIGN_KEYCHAIN_PASSWORD")
	identifier := strings.TrimSpace(os.Getenv("AGENTDOCK_CODESIGN_IDENTIFIER"))
	if identifier == "" {
		identifier = "com.local.agentdock"
	}
	signHome := strings.TrimSpace(os.Getenv("AGENTDOCK_CODESIGN_HOME"))
	if signHome == "" {
		signHome = home
	}
	signHomeInfo, err := os.Stat(signHome)
	if err != nil || !signHomeInfo.IsDir() {
		return fmt.Errorf("AGENTDOCK_CODESIGN_HOME 不是可用目录: %s", signHome)
	}

	// 没有本地身份时保留 Release 自带签名，但仍拒绝启动签名损坏的二进制。
	if identity == "" {
		output, verifyErr := exec.CommandContext(ctx, "codesign", "--verify", "--strict", "--verbose=2", targetPath).CombinedOutput()
		if verifyErr != nil {
			return fmt.Errorf("Release 代码签名验证失败: %w: %s", verifyErr, strings.TrimSpace(string(output)))
		}
		return nil
	}

	if keychain != "" && !regularFile(keychain) {
		return fmt.Errorf("代码签名钥匙串不存在或不是普通文件: %s", keychain)
	}
	commandEnv := append(os.Environ(), "HOME="+signHome)
	if keychain != "" {
		unlockCommand := exec.CommandContext(ctx, "security", "unlock-keychain", "-p", keychainPassword, keychain)
		unlockCommand.Env = commandEnv
		if output, unlockErr := unlockCommand.CombinedOutput(); unlockErr != nil {
			return fmt.Errorf("解锁 macOS 代码签名钥匙串失败: %w: %s", unlockErr, strings.TrimSpace(string(output)))
		}
	}
	identityArgs := []string{"find-identity", "-v", "-p", "codesigning"}
	if keychain != "" {
		identityArgs = append(identityArgs, keychain)
	}
	identityCommand := exec.CommandContext(ctx, "security", identityArgs...)
	identityCommand.Env = commandEnv
	identityOutput, err := identityCommand.CombinedOutput()
	if err != nil {
		return fmt.Errorf("读取 macOS 代码签名身份失败: %w: %s", err, strings.TrimSpace(string(identityOutput)))
	}
	if !strings.Contains(string(identityOutput), identity) {
		return fmt.Errorf("找不到 macOS 代码签名身份 %s: %s", identity, strings.TrimSpace(string(identityOutput)))
	}

	codesignArgs := []string{"--force"}
	if keychain != "" {
		codesignArgs = append(codesignArgs, "--keychain", keychain)
	}
	codesignArgs = append(codesignArgs,
		"--sign", identity,
		"--timestamp=none",
		"--options", "runtime",
		"--identifier", identifier,
		targetPath,
	)
	codesignCommand := exec.CommandContext(ctx, "codesign", codesignArgs...)
	codesignCommand.Env = commandEnv
	if output, signErr := codesignCommand.CombinedOutput(); signErr != nil {
		return fmt.Errorf("macOS 本地签名失败: %w: %s", signErr, strings.TrimSpace(string(output)))
	}
	verifyCommand := exec.CommandContext(ctx, "codesign", "--verify", "--strict", "--verbose=2", targetPath)
	verifyCommand.Env = commandEnv
	if output, verifyErr := verifyCommand.CombinedOutput(); verifyErr != nil {
		return fmt.Errorf("macOS 本地签名验证失败: %w: %s", verifyErr, strings.TrimSpace(string(output)))
	}
	detailCommand := exec.CommandContext(ctx, "codesign", "-dv", "--verbose=4", targetPath)
	detailCommand.Env = commandEnv
	detailOutput, detailErr := detailCommand.CombinedOutput()
	if detailErr != nil {
		return fmt.Errorf("读取 macOS 签名详情失败: %w: %s", detailErr, strings.TrimSpace(string(detailOutput)))
	}
	if !containsCodeSignIdentifier(string(detailOutput), identifier) {
		return fmt.Errorf("macOS 签名 Identifier 验证失败，期望 %s: %s", identifier, strings.TrimSpace(string(detailOutput)))
	}
	return nil
}

func regularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func executableRegularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func containsCodeSignIdentifier(output, identifier string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "Identifier="+identifier {
			return true
		}
	}
	return false
}
