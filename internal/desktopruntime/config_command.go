package desktopruntime

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
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
	ACPAgent                string
	ACPCommand              string
	ACPArgs                 []string
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
		acpAgent := flags.String("acp-agent", "codex", "Coding Agent 预设")
		acpCommand := flags.String("acp-command", "", "自定义 ACP Adapter 可执行文件绝对路径")
		acpArgsJSON := flags.String("acp-args-json", "[]", "自定义 ACP Adapter 参数 JSON 字符串数组")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return configCommandUsageError()
		}
		var acpArgs []string
		if err := json.Unmarshal([]byte(*acpArgsJSON), &acpArgs); err != nil {
			return fmt.Errorf("解析 Coding Agent 参数失败: %w", err)
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
			ACPAgent:                strings.ToLower(strings.TrimSpace(*acpAgent)),
			ACPCommand:              strings.TrimSpace(*acpCommand),
			ACPArgs:                 acpArgs,
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
	switch request.ACPAgent {
	case "codex", "claude", "grok":
	case "custom":
		if request.ACPEnabled && request.ACPCommand == "" {
			return errors.New("自定义 Coding Agent 必须填写 ACP Adapter 命令")
		}
	default:
		return fmt.Errorf("不支持的 Coding Agent: %s", request.ACPAgent)
	}
	return nil
}

func configCommandUsageError() error {
	return errors.New("用法：agentdock config update --runtime-root <目录> --port <端口> --log-level <级别> [高级设置]")
}
