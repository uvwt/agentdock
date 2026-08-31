//go:build windows

package desktopruntime

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	agentconfig "github.com/uvwt/agentdock/internal/config"
	"golang.org/x/sys/windows"
)

var managedCoreEnvironment = []string{
	"AGENTDOCK_AUTH_TOKEN",
	"AGENTDOCK_HOST",
	"AGENTDOCK_PORT",
	"AGENTDOCK_LOG_LEVEL",
	// 仅用于清除旧服务环境，核心不再读取这两个配置。
	"AGENTDOCK_NEXUS_ENDPOINT",
	"AGENTDOCK_NEXUS_TOKEN",
	"AGENTDOCK_BROWSER_ENABLED",
	"AGENTDOCK_BROWSER_CDP_URL",
	"AGENTDOCK_BROWSER_REUSE_EXISTING_CDP",
	"AGENTDOCK_ACP_ENABLED",
	"AGENTDOCK_ACP_AGENT",
	"AGENTDOCK_ACP_COMMAND",
	"AGENTDOCK_ACP_ARGS_JSON",
	"AGENTDOCK_ACP_ENV_FROM_ENV_JSON",
	"AGENTDOCK_ACP_ALLOWED_ROOTS",
	"AGENTDOCK_ACP_MAX_CONCURRENT_PROMPTS",
	"AGENTDOCK_ACP_INTERACTION_TIMEOUT_MS",
	"AGENTDOCK_SERVER_URL",
	"AGENTDOCK_OAUTH_ENABLED",
	"AGENTDOCK_OAUTH_PASSWORD",
	"AGENTDOCK_OAUTH_TOKEN_SECRET",
	"AGENTDOCK_OAUTH_ACCESS_TOKEN_TTL",
}

type controlPanelSettings struct {
	Port                    int      `json:"port"`
	LogLevel                string   `json:"log_level"`
	OAuthAccessTokenTTL     string   `json:"oauth_access_token_ttl,omitempty"`
	BrowserEnabled          bool     `json:"browser_enabled"`
	BrowserCDPURL           string   `json:"browser_cdp_url"`
	BrowserReuseExistingCDP bool     `json:"browser_reuse_existing_cdp"`
	ACPEnabled              bool     `json:"acp_enabled"`
	ACPAgent                string   `json:"acp_agent"`
	ACPCommand              string   `json:"acp_command"`
	ACPArgs                 []string `json:"acp_args"`
}

func platformPrepareCoreEnvironment(runtimeRoot string) error {
	manifest, root, err := loadDesktopManifest(runtimeRoot)
	if err != nil {
		return err
	}
	settings, err := loadControlPanelSettings(root, manifest.Port)
	if err != nil {
		return err
	}
	inheritedOAuthAccessTokenTTL := strings.TrimSpace(os.Getenv("AGENTDOCK_OAUTH_ACCESS_TOKEN_TTL"))

	for _, name := range managedCoreEnvironment {
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("清理 %s 失败: %w", name, err)
		}
	}
	// Device Token 已由 ~/.agentdock/nexus/device.json 唯一管理；旧 DPAPI Token 不再读取并立即清除。
	legacyNexusTokenPath := filepath.Join(root, "nexus-token.dpapi")
	if err := os.Remove(legacyNexusTokenPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("删除已废弃的 Nexus Token 失败: %w", err)
	}
	authToken, err := readProtectedText(filepath.Join(root, "auth-token.dpapi"), "agentdock.startup.v1")
	if err != nil {
		return fmt.Errorf("读取 Bearer Token 失败: %w", err)
	}
	if strings.TrimSpace(authToken) == "" {
		return errors.New("Bearer Token 为空，请运行 Setup.exe 修复安装")
	}

	managed := map[string]string{
		"AGENTDOCK_RUNTIME_ROOT":               root,
		"AGENTDOCK_AUTH_TOKEN":                 authToken,
		"AGENTDOCK_HOST":                       "127.0.0.1",
		"AGENTDOCK_PORT":                       strconv.Itoa(settings.Port),
		"AGENTDOCK_LOG_LEVEL":                  settings.LogLevel,
		"AGENTDOCK_BROWSER_ENABLED":            strconv.FormatBool(settings.BrowserEnabled),
		"AGENTDOCK_BROWSER_REUSE_EXISTING_CDP": strconv.FormatBool(settings.BrowserReuseExistingCDP),
		"AGENTDOCK_ACP_ENABLED":                strconv.FormatBool(settings.ACPEnabled),
	}
	if settings.BrowserCDPURL != "" {
		managed["AGENTDOCK_BROWSER_CDP_URL"] = settings.BrowserCDPURL
	}
	if settings.ACPEnabled {
		info, statErr := os.Stat(settings.ACPCommand)
		if statErr != nil {
			return fmt.Errorf("读取 Coding Agent 命令失败 %s: %w", settings.ACPCommand, statErr)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Coding Agent 命令不是普通文件: %s", settings.ACPCommand)
		}
		argsJSON, marshalErr := json.Marshal(settings.ACPArgs)
		if marshalErr != nil {
			return fmt.Errorf("编码 Coding Agent 参数失败: %w", marshalErr)
		}
		managed["AGENTDOCK_ACP_AGENT"] = settings.ACPAgent
		managed["AGENTDOCK_ACP_COMMAND"] = settings.ACPCommand
		managed["AGENTDOCK_ACP_ARGS_JSON"] = string(argsJSON)
	}

	serverURL, err := readTrimmedText(filepath.Join(root, "server-url.txt"))
	if err != nil {
		return err
	}
	if serverURL != "" {
		serverURL, err = normalizeHTTPSOrigin(serverURL)
		if err != nil {
			return err
		}
		if err := writeRuntimeText(filepath.Join(root, "server-url.txt"), serverURL); err != nil {
			return err
		}
		oauthPassword, passwordErr := readProtectedText(filepath.Join(root, "oauth-password.dpapi"), "agentdock.oauth.password.v1")
		if passwordErr != nil {
			return fmt.Errorf("读取 OAuth 密码失败: %w", passwordErr)
		}
		oauthSecret, secretErr := readProtectedText(filepath.Join(root, "oauth-token-secret.dpapi"), "agentdock.oauth.secret.v1")
		if secretErr != nil {
			return fmt.Errorf("读取 OAuth 签名密钥失败: %w", secretErr)
		}
		managed["AGENTDOCK_SERVER_URL"] = serverURL
		managed["AGENTDOCK_OAUTH_ENABLED"] = "true"
		managed["AGENTDOCK_OAUTH_PASSWORD"] = oauthPassword
		managed["AGENTDOCK_OAUTH_TOKEN_SECRET"] = oauthSecret
	}
	oauthAccessTokenTTL := effectiveOAuthAccessTokenTTL(settings.OAuthAccessTokenTTL, inheritedOAuthAccessTokenTTL)
	if oauthAccessTokenTTL != "" {
		managed["AGENTDOCK_OAUTH_ACCESS_TOKEN_TTL"] = oauthAccessTokenTTL
	}

	for name, value := range managed {
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("设置 %s 失败: %w", name, err)
		}
	}
	return nil
}

func loadControlPanelSettings(runtimeRoot string, fallbackPort int) (controlPanelSettings, error) {
	settings := controlPanelSettings{Port: fallbackPort, LogLevel: "info", ACPAgent: "codex"}
	data, err := os.ReadFile(filepath.Join(runtimeRoot, "control-panel-settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return controlPanelSettings{}, fmt.Errorf("读取控制面板设置失败: %w", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return controlPanelSettings{}, fmt.Errorf("解析控制面板设置失败: %w", err)
	}
	if settings.Port < 1 || settings.Port > 65535 {
		return controlPanelSettings{}, fmt.Errorf("控制面板端口超出范围: %d", settings.Port)
	}
	settings.LogLevel = strings.ToLower(strings.TrimSpace(settings.LogLevel))
	if settings.LogLevel == "" {
		settings.LogLevel = "info"
	}
	if settings.LogLevel != "debug" && settings.LogLevel != "info" && settings.LogLevel != "warn" && settings.LogLevel != "error" {
		return controlPanelSettings{}, fmt.Errorf("不支持的日志级别: %s", settings.LogLevel)
	}
	settings.OAuthAccessTokenTTL = strings.TrimSpace(settings.OAuthAccessTokenTTL)
	if settings.OAuthAccessTokenTTL != "" {
		if err := agentconfig.ValidateOAuthAccessTokenTTL(settings.OAuthAccessTokenTTL); err != nil {
			return controlPanelSettings{}, fmt.Errorf("OAuth Access Token 有效期无效: %w", err)
		}
	}
	settings.ACPAgent = strings.ToLower(strings.TrimSpace(settings.ACPAgent))
	if settings.ACPAgent == "" {
		settings.ACPAgent = "codex"
	}
	settings.ACPCommand = strings.TrimSpace(settings.ACPCommand)
	if settings.ACPCommand != "" {
		settings.ACPCommand = filepath.Clean(settings.ACPCommand)
	}
	switch settings.ACPAgent {
	case "codex", "claude", "grok", "custom":
	default:
		return controlPanelSettings{}, fmt.Errorf("不支持的 Coding Agent: %s", settings.ACPAgent)
	}
	if settings.ACPEnabled && !filepath.IsAbs(settings.ACPCommand) {
		return controlPanelSettings{}, fmt.Errorf("Coding Agent 命令必须是绝对路径: %s", settings.ACPCommand)
	}
	return settings, nil
}

func effectiveOAuthAccessTokenTTL(configured, inherited string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	return strings.TrimSpace(inherited)
}

func readProtectedText(path, entropy string) (string, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		return "", fmt.Errorf("解析 DPAPI 数据失败: %w", err)
	}
	if len(ciphertext) == 0 {
		return "", errors.New("DPAPI 数据为空")
	}
	entropyBytes := []byte(entropy)
	input := windows.DataBlob{Size: uint32(len(ciphertext)), Data: &ciphertext[0]}
	optionalEntropy := windows.DataBlob{Size: uint32(len(entropyBytes)), Data: &entropyBytes[0]}
	var output windows.DataBlob
	if err := windows.CryptUnprotectData(&input, nil, &optionalEntropy, 0, nil, 0, &output); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	plain := unsafe.Slice(output.Data, int(output.Size))
	return string(append([]byte(nil), plain...)), nil
}

func readTrimmedText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}
