package desktopruntime

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	agentconfig "github.com/uvwt/agentdock/internal/config"
)

// ConfigUpdateRequest 是桌面端保存日常运行设置时使用的结构化请求。
type ConfigUpdateRequest struct {
	RuntimeRoot             string
	Port                    int
	LogLevel                string
	OAuthAccessTokenTTL     string
	MCPAppsEnabled          bool
	BrowserEnabled          bool
	BrowserCDPURL           string
	BrowserReuseExistingCDP bool
	ACPEnabled              bool
	ACPProfiles             []agentconfig.ACPProfile
	ACPDefaultProfile       string
}

func RunConfigCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return configCommandUsageError()
	}
	switch args[0] {
	case "update":
		flags := flag.NewFlagSet("agentdock config update", flag.ContinueOnError)
		flags.SetOutput(stderr)
		runtimeRoot := flags.String("runtime-root", "", "桌面运行目录")
		port := flags.Int("port", 0, "本地监听端口")
		logLevel := flags.String("log-level", "info", "日志级别")
		oauthAccessTokenTTL := flags.String("oauth-access-token-ttl", "", "OAuth Access Token 有效期；留空表示继承环境变量或使用默认值")
		mcpAppsEnabled := flags.Bool("mcp-apps-enabled", true, "启用 MCP Apps UI")
		browserEnabled := flags.Bool("browser-enabled", false, "启用浏览器")
		browserCDPURL := flags.String("browser-cdp-url", "", "已有 Chromium CDP 地址")
		browserReuseExistingCDP := flags.Bool("browser-reuse-existing-cdp", false, "自动发现并复用唯一已有 CDP")
		acpEnabled := flags.Bool("acp-enabled", false, "启用 Coding Agent")
		acpProfilesJSON := flags.String("acp-profiles-json", "", "多个 ACP Profile 的 JSON 数组")
		acpDefaultProfile := flags.String("acp-default-profile", "", "默认 ACP Profile ID")
		acpAgent := flags.String("acp-agent", "codex", "Coding Agent 预设")
		acpCommand := flags.String("acp-command", "", "自定义 ACP Adapter 可执行文件绝对路径")
		acpArgsJSON := flags.String("acp-args-json", "[]", "自定义 ACP Adapter 参数 JSON 字符串数组")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return configCommandUsageError()
		}
		var acpProfiles []agentconfig.ACPProfile
		if raw := strings.TrimSpace(*acpProfilesJSON); raw != "" {
			if err := json.Unmarshal([]byte(raw), &acpProfiles); err != nil {
				return fmt.Errorf("解析 ACP Profiles 失败: %w", err)
			}
		} else {
			// 旧 CLI 参数只在命令入口兼容，立即转换成 Profile，后续不再保留单 ACP 字段。
			var legacyArgs []string
			if err := json.Unmarshal([]byte(*acpArgsJSON), &legacyArgs); err != nil {
				return fmt.Errorf("解析 Coding Agent 参数失败: %w", err)
			}
			legacyKind := strings.ToLower(strings.TrimSpace(*acpAgent))
			acpProfiles = []agentconfig.ACPProfile{{
				ID: legacyKind, Kind: legacyKind, Command: strings.TrimSpace(*acpCommand), Args: legacyArgs, Enabled: true,
			}}
			if strings.TrimSpace(*acpDefaultProfile) == "" {
				*acpDefaultProfile = legacyKind
			}
		}
		request := ConfigUpdateRequest{
			RuntimeRoot:             strings.TrimSpace(*runtimeRoot),
			Port:                    *port,
			LogLevel:                strings.ToLower(strings.TrimSpace(*logLevel)),
			OAuthAccessTokenTTL:     strings.TrimSpace(*oauthAccessTokenTTL),
			MCPAppsEnabled:          *mcpAppsEnabled,
			BrowserEnabled:          *browserEnabled,
			BrowserCDPURL:           strings.TrimSpace(*browserCDPURL),
			BrowserReuseExistingCDP: *browserReuseExistingCDP,
			ACPEnabled:              *acpEnabled,
			ACPProfiles:             acpProfiles,
			ACPDefaultProfile:       strings.TrimSpace(*acpDefaultProfile),
		}
		if err := validateConfigUpdate(request); err != nil {
			return err
		}
		if err := platformUpdateConfig(ctx, request); err != nil {
			return err
		}
		_, err := fmt.Fprintln(stdout, `{"updated":true}`)
		return err
	default:
		return configCommandUsageError()
	}
}

func validateConfigUpdate(request ConfigUpdateRequest) error {
	if request.RuntimeRoot == "" {
		return errors.New("runtime-root 不能为空")
	}
	if request.Port < 1 || request.Port > 65535 {
		return errors.New("端口必须是 1 到 65535 之间的整数")
	}
	switch request.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("不支持的日志级别: %s", request.LogLevel)
	}
	if request.OAuthAccessTokenTTL != "" {
		if err := agentconfig.ValidateOAuthAccessTokenTTL(request.OAuthAccessTokenTTL); err != nil {
			return fmt.Errorf("OAuth Access Token 有效期无效: %w", err)
		}
	}
	if request.BrowserCDPURL != "" {
		parsed, err := url.Parse(request.BrowserCDPURL)
		if err != nil || parsed.Host == "" {
			return errors.New("浏览器 CDP 地址必须是有效的绝对 URL")
		}
		if parsed.User != nil || parsed.Fragment != "" {
			return errors.New("浏览器 CDP 地址不能包含账号信息或片段")
		}
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "ws", "wss":
		default:
			return errors.New("浏览器 CDP 地址必须使用 http、https、ws 或 wss")
		}
	}
	if len(request.ACPProfiles) == 0 {
		if request.ACPEnabled {
			return errors.New("启用 Coding Agent 时至少需要一个 ACP Profile")
		}
		return nil
	}
	return validateConfigACPProfiles(request)
}

func validateConfigACPProfiles(request ConfigUpdateRequest) error {
	seen := make(map[string]struct{}, len(request.ACPProfiles))
	enabled := make(map[string]struct{}, len(request.ACPProfiles))
	for _, raw := range request.ACPProfiles {
		id := strings.TrimSpace(raw.ID)
		kind := strings.ToLower(strings.TrimSpace(raw.Kind))
		if !validACPProfileIdentifier(id) {
			return fmt.Errorf("ACP Profile ID 必须是 1-64 位字母、数字、点、下划线或连字符: %q", id)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("ACP Profile ID 重复: %s", id)
		}
		seen[id] = struct{}{}
		switch kind {
		case "codex", "claude", "grok":
			if id != kind {
				return fmt.Errorf("内置 ACP %s 必须使用固定 Profile ID %s", kind, kind)
			}
		case "custom":
			if id == "codex" || id == "claude" || id == "grok" {
				return fmt.Errorf("自定义 ACP Profile ID %s 已被内置 ACP 保留", id)
			}
		default:
			return fmt.Errorf("不支持的 ACP Profile 类型: %s", kind)
		}
		if !raw.Enabled {
			continue
		}
		enabled[id] = struct{}{}
		if request.ACPEnabled {
			command := strings.TrimSpace(raw.Command)
			if kind == "custom" && (command == "" || !filepath.IsAbs(command)) {
				return fmt.Errorf("启用的自定义 ACP Profile %s 必须配置绝对路径命令", id)
			}
			if command != "" && !filepath.IsAbs(command) {
				return fmt.Errorf("ACP Profile %s 命令必须是绝对路径", id)
			}
		}
	}
	if request.ACPEnabled && len(enabled) == 0 {
		return errors.New("启用 Coding Agent 时至少需要一个启用的 ACP Profile")
	}
	if request.ACPDefaultProfile != "" {
		if _, exists := enabled[request.ACPDefaultProfile]; !exists && request.ACPEnabled {
			return fmt.Errorf("默认 ACP Profile 必须引用已启用 Profile: %s", request.ACPDefaultProfile)
		}
	}
	return nil
}

func validACPProfileIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '.', char == '_', char == '-':
		default:
			return false
		}
	}
	return true
}

func configCommandUsageError() error {
	return errors.New("用法：agentdock config update --runtime-root <目录> --port <端口> --log-level <级别> [高级设置]")
}
