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
	"AGENTDOCK_MCP_APPS_ENABLED",
	// 仅用于清除旧服务环境，核心不再读取这两个配置。
	"AGENTDOCK_NEXUS_ENDPOINT",
	"AGENTDOCK_NEXUS_TOKEN",
	"AGENTDOCK_BROWSER_ENABLED",
	"AGENTDOCK_BROWSER_CDP_URL",
	"AGENTDOCK_BROWSER_REUSE_EXISTING_CDP",
	"AGENTDOCK_ACP_ENABLED",
	"AGENTDOCK_ACP_PROFILES_JSON",
	"AGENTDOCK_ACP_DEFAULT_PROFILE",
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
	Port                    int                      `json:"port"`
	LogLevel                string                   `json:"log_level"`
	OAuthAccessTokenTTL     string                   `json:"oauth_access_token_ttl,omitempty"`
	MCPAppsEnabled          bool                     `json:"mcp_apps_enabled"`
	BrowserEnabled          bool                     `json:"browser_enabled"`
	BrowserCDPURL           string                   `json:"browser_cdp_url"`
	BrowserReuseExistingCDP bool                     `json:"browser_reuse_existing_cdp"`
	ACPEnabled              bool                     `json:"acp_enabled"`
	ACPProfiles             []agentconfig.ACPProfile `json:"acp_profiles,omitempty"`
	ACPDefaultProfile       string                   `json:"acp_default_profile,omitempty"`
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
	authToken, err := readOrCreateProtectedText(filepath.Join(root, "auth-token.dpapi"), "agentdock.startup.v1", 32, "Bearer Token")
	if err != nil {
		return err
	}

	managed := map[string]string{
		"AGENTDOCK_RUNTIME_ROOT":               root,
		"AGENTDOCK_AUTH_TOKEN":                 authToken,
		"AGENTDOCK_HOST":                       "127.0.0.1",
		"AGENTDOCK_PORT":                       strconv.Itoa(settings.Port),
		"AGENTDOCK_LOG_LEVEL":                  settings.LogLevel,
		"AGENTDOCK_MCP_APPS_ENABLED":           strconv.FormatBool(settings.MCPAppsEnabled),
		"AGENTDOCK_BROWSER_ENABLED":            strconv.FormatBool(settings.BrowserEnabled),
		"AGENTDOCK_BROWSER_REUSE_EXISTING_CDP": strconv.FormatBool(settings.BrowserReuseExistingCDP),
		"AGENTDOCK_ACP_ENABLED":                strconv.FormatBool(settings.ACPEnabled),
	}
	if path := strings.TrimSpace(manifest.AgentDockHome); path != "" {
		managed["AGENTDOCK_HOME"] = filepath.Clean(path)
	}
	if path := strings.TrimSpace(manifest.AgentDockDefaultDir); path != "" {
		managed["AGENTDOCK_DEFAULT_DIR"] = filepath.Clean(path)
	}
	if settings.BrowserCDPURL != "" {
		managed["AGENTDOCK_BROWSER_CDP_URL"] = settings.BrowserCDPURL
	}
	if settings.ACPEnabled {
		if len(settings.ACPProfiles) == 0 {
			return errors.New("启用 Coding Agent 时至少需要一个 ACP Profile")
		}
		for _, profile := range settings.ACPProfiles {
			if !profile.Enabled {
				continue
			}
			info, statErr := os.Stat(profile.Command)
			if statErr != nil {
				return fmt.Errorf("读取 ACP Profile %s 命令失败 %s: %w", profile.ID, profile.Command, statErr)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("ACP Profile %s 命令不是普通文件: %s", profile.ID, profile.Command)
			}
		}
		profilesJSON, marshalErr := json.Marshal(settings.ACPProfiles)
		if marshalErr != nil {
			return fmt.Errorf("编码 ACP Profiles 失败: %w", marshalErr)
		}
		managed["AGENTDOCK_ACP_PROFILES_JSON"] = string(profilesJSON)
		managed["AGENTDOCK_ACP_DEFAULT_PROFILE"] = settings.ACPDefaultProfile
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
		oauthPassword, passwordErr := readOrCreateProtectedText(filepath.Join(root, "oauth-password.dpapi"), "agentdock.oauth.password.v1", 12, "OAuth 密码")
		if passwordErr != nil {
			return passwordErr
		}
		oauthSecret, secretErr := readOrCreateProtectedText(filepath.Join(root, "oauth-token-secret.dpapi"), "agentdock.oauth.secret.v1", 32, "OAuth 签名密钥")
		if secretErr != nil {
			return secretErr
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
	settings := controlPanelSettings{Port: fallbackPort, LogLevel: "info", MCPAppsEnabled: true}
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
	if len(settings.ACPProfiles) == 0 {
		// 旧 control-panel-settings.json 只在读取边界迁移一次；新文件只保存 Profiles。
		var legacy struct {
			Agent   string   `json:"acp_agent"`
			Command string   `json:"acp_command"`
			Args    []string `json:"acp_args"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return controlPanelSettings{}, fmt.Errorf("解析旧 ACP 控制面板设置失败: %w", err)
		}
		legacy.Agent = strings.ToLower(strings.TrimSpace(legacy.Agent))
		if legacy.Agent != "" {
			if legacy.Agent != "codex" && legacy.Agent != "claude" && legacy.Agent != "grok" && legacy.Agent != "custom" {
				return controlPanelSettings{}, fmt.Errorf("不支持的 Coding Agent: %s", legacy.Agent)
			}
			settings.ACPProfiles = []agentconfig.ACPProfile{{
				ID: legacy.Agent, Kind: legacy.Agent, Command: strings.TrimSpace(legacy.Command),
				Args: append([]string(nil), legacy.Args...), Enabled: true,
			}}
			settings.ACPDefaultProfile = legacy.Agent
		}
	}
	if len(settings.ACPProfiles) > 0 {
		firstEnabled := ""
		for index := range settings.ACPProfiles {
			profile := &settings.ACPProfiles[index]
			profile.ID = strings.TrimSpace(profile.ID)
			profile.Kind = strings.ToLower(strings.TrimSpace(profile.Kind))
			profile.Command = strings.TrimSpace(profile.Command)
			if profile.Command != "" {
				profile.Command = filepath.Clean(profile.Command)
			}
			if profile.Enabled && firstEnabled == "" {
				firstEnabled = profile.ID
			}
		}
		settings.ACPDefaultProfile = strings.TrimSpace(settings.ACPDefaultProfile)
		if settings.ACPDefaultProfile == "" {
			settings.ACPDefaultProfile = firstEnabled
		}
		request := ConfigUpdateRequest{
			RuntimeRoot: runtimeRoot, Port: settings.Port, LogLevel: settings.LogLevel,
			ACPEnabled: settings.ACPEnabled, ACPProfiles: settings.ACPProfiles, ACPDefaultProfile: settings.ACPDefaultProfile,
		}
		if err := validateConfigACPProfiles(request); err != nil {
			return controlPanelSettings{}, err
		}
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
