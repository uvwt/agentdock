//go:build darwin

package selfupdate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type localCodeSignConfig struct {
	identity         string
	keychain         string
	keychainPassword string
	home             string
}

func localCodeSignConfigFromEnvironment(defaultHome string) (localCodeSignConfig, error) {
	config := localCodeSignConfig{
		identity:         strings.TrimSpace(os.Getenv("AGENTDOCK_CODESIGN_IDENTITY")),
		keychain:         strings.TrimSpace(os.Getenv("AGENTDOCK_CODESIGN_KEYCHAIN")),
		keychainPassword: os.Getenv("AGENTDOCK_CODESIGN_KEYCHAIN_PASSWORD"),
		home:             strings.TrimSpace(os.Getenv("AGENTDOCK_CODESIGN_HOME")),
	}
	if config.identity == "" {
		return config, nil
	}
	if config.home == "" {
		config.home = defaultHome
	}
	homeInfo, err := os.Stat(config.home)
	if err != nil || !homeInfo.IsDir() {
		return localCodeSignConfig{}, fmt.Errorf("AGENTDOCK_CODESIGN_HOME 不是可用目录: %s", config.home)
	}
	if config.keychain != "" && !regularFile(config.keychain) {
		return localCodeSignConfig{}, fmt.Errorf("代码签名钥匙串不存在或不是普通文件: %s", config.keychain)
	}
	return config, nil
}

func (config localCodeSignConfig) enabled() bool {
	return config.identity != ""
}

func (config localCodeSignConfig) prepare(ctx context.Context) error {
	if !config.enabled() {
		return nil
	}
	commandEnv := environmentWithOverride(os.Environ(), "HOME", config.home)
	if config.keychain != "" {
		unlockCommand := exec.CommandContext(ctx, "security", "unlock-keychain", "-p", config.keychainPassword, config.keychain)
		unlockCommand.Env = commandEnv
		if output, err := unlockCommand.CombinedOutput(); err != nil {
			return fmt.Errorf("解锁 macOS 代码签名钥匙串失败: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}

	identityArgs := []string{"find-identity", "-v", "-p", "codesigning"}
	if config.keychain != "" {
		identityArgs = append(identityArgs, config.keychain)
	}
	identityCommand := exec.CommandContext(ctx, "security", identityArgs...)
	identityCommand.Env = commandEnv
	identityOutput, err := identityCommand.CombinedOutput()
	if err != nil {
		return fmt.Errorf("读取 macOS 代码签名身份失败: %w: %s", err, strings.TrimSpace(string(identityOutput)))
	}
	if !strings.Contains(string(identityOutput), config.identity) {
		return fmt.Errorf("找不到 macOS 代码签名身份 %s: %s", config.identity, strings.TrimSpace(string(identityOutput)))
	}
	return nil
}

func (config localCodeSignConfig) sign(ctx context.Context, targetPath, identifier string) error {
	codesignArgs := []string{"--force"}
	if config.keychain != "" {
		codesignArgs = append(codesignArgs, "--keychain", config.keychain)
	}
	codesignArgs = append(codesignArgs,
		"--sign", config.identity,
		"--timestamp=none",
		"--options", "runtime",
		"--identifier", identifier,
		targetPath,
	)
	commandEnv := environmentWithOverride(os.Environ(), "HOME", config.home)
	codesignCommand := exec.CommandContext(ctx, "codesign", codesignArgs...)
	codesignCommand.Env = commandEnv
	if output, err := codesignCommand.CombinedOutput(); err != nil {
		return fmt.Errorf("macOS 本地签名失败: %w: %s", err, strings.TrimSpace(string(output)))
	}

	verifyCommand := exec.CommandContext(ctx, "codesign", "--verify", "--strict", "--verbose=2", targetPath)
	verifyCommand.Env = commandEnv
	if output, err := verifyCommand.CombinedOutput(); err != nil {
		return fmt.Errorf("macOS 本地签名验证失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	detailCommand := exec.CommandContext(ctx, "codesign", "-dv", "--verbose=4", targetPath)
	detailCommand.Env = commandEnv
	detailOutput, err := detailCommand.CombinedOutput()
	if err != nil {
		return fmt.Errorf("读取 macOS 签名详情失败: %w: %s", err, strings.TrimSpace(string(detailOutput)))
	}
	if !containsCodeSignIdentifier(string(detailOutput), identifier) {
		return fmt.Errorf("macOS 签名 Identifier 验证失败，期望 %s: %s", identifier, strings.TrimSpace(string(detailOutput)))
	}
	return nil
}

func signLocalDesktopReplacement(ctx context.Context, appPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	config, err := localCodeSignConfigFromEnvironment(home)
	if err != nil {
		return err
	}
	if !config.enabled() {
		return nil
	}
	if err := config.prepare(ctx); err != nil {
		return err
	}

	// TCC 会同时把外层 App 和实际执行桌面能力的 Core 当作责任主体。
	// 因此稳定签名必须覆盖所有嵌套可执行文件，再最后签外层 App；
	// 只重签 Core 或只重签 App 都无法保证升级后继续匹配原有权限记录。
	for _, target := range []struct {
		path       string
		identifier string
	}{
		{path: filepath.Join(appPath, "Contents", "Helpers", "AgentDockLoginHelper"), identifier: "com.uvwt.agentdock.login-helper"},
		{path: filepath.Join(appPath, "Contents", "Helpers", "agentdock"), identifier: "com.uvwt.agentdock.core"},
		{path: filepath.Join(appPath, "Contents", "Helpers", "cloudflared"), identifier: "com.uvwt.agentdock.cloudflared"},
		{path: appPath, identifier: "com.uvwt.agentdock"},
	} {
		if err := config.sign(ctx, target.path, target.identifier); err != nil {
			return fmt.Errorf("重签 %s 失败: %w", target.identifier, err)
		}
	}

	verifyCommand := exec.CommandContext(ctx, "codesign", "--verify", "--deep", "--strict", "--verbose=2", appPath)
	verifyCommand.Env = environmentWithOverride(os.Environ(), "HOME", config.home)
	if output, err := verifyCommand.CombinedOutput(); err != nil {
		return fmt.Errorf("macOS App 本地稳定签名递归验证失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
