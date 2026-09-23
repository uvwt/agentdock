package httpx

import (
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/runtimeapi"
)

const (
	agentDockRepositoryURL = "https://github.com/uvwt/agentdock"
	agentDockDocsURL       = "https://uvwt.github.io/agentdock-docs/"
	agentDockQQGroup       = "1081337019"
	agentDockQQGroupURL    = "https://qun.qq.com/universal-share/share?ac=1&authKey=Rp86bSzI7vqm87KoYlKawgsPZ440Ubhyezw6Qkgcn3JISwX3zXxsXkbS5598RrY5&busi_data=eyJncm91cENvZGUiOiIxMDgxMzM3MDE5IiwidG9rZW4iOiJ0Mlg1bUU1ZWtuZzF3SHJDT3pSaGsrOURIMlNYaXBlYllOUjNLZ1BUb1hzM2lJSTZjeVNldzU0ajl0SjRVZkx2IiwidWluIjoiMzIwMjA4ODAzMiJ9&data=W28mWvuqaLf_Fwnf0CgAJXuDs6l3A78V7AoWZnizPboCpKoQMzHzZ-UlluYo47U3tmIBHK2xIgWEVEJbTiGsPQ&svctype=4&tempid=h5_group_info"
	statusPageCSP          = "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'"
)

// 状态页保持完全自包含，避免公开入口依赖第三方静态资源或额外前端构建链路。
//
//go:embed status_page.html
var statusPageHTML string

var statusPageTemplate = template.Must(template.New("status-page").Parse(statusPageHTML))

type statusPageText struct {
	Lang              string
	Subtitle          string
	Online            string
	ReadyTitle        string
	ReadyDescription  string
	Version           string
	System            string
	Capabilities      string
	Tools             string
	Plugins           string
	MCPReady          string
	Browser           string
	Auth              string
	Enabled           string
	Disabled          string
	None              string
	Token             string
	OAuthAndToken     string
	MCPEndpoint       string
	Copy              string
	Copied            string
	CopyFailed        string
	EndpointHint      string
	Resources         string
	Repository        string
	RepositoryDesc    string
	Documentation     string
	DocumentationDesc string
	QQGroup           string
	QQGroupDesc       string
	OpenSource        string
	License           string
	DocumentationURL  string
}

var statusPageEnglish = statusPageText{
	Lang:              "en",
	Subtitle:          "AI Agent device runtime",
	Online:            "Online",
	ReadyTitle:        "Ready for AI agents.",
	ReadyDescription:  "This AgentDock instance is online and ready to expose local capabilities through MCP.",
	Version:           "Version",
	System:            "System",
	Capabilities:      "Capabilities",
	Tools:             "Tools",
	Plugins:           "Plugins",
	MCPReady:          "Ready",
	Browser:           "Browser",
	Auth:              "Auth",
	Enabled:           "Enabled",
	Disabled:          "Disabled",
	None:              "None",
	Token:             "Token",
	OAuthAndToken:     "OAuth + Token",
	MCPEndpoint:       "MCP Endpoint",
	Copy:              "Copy",
	Copied:            "Copied",
	CopyFailed:        "Copy failed",
	EndpointHint:      "Use this endpoint to connect AgentDock with an MCP client.",
	Resources:         "Resources",
	Repository:        "GitHub Repository",
	RepositoryDesc:    "Source code, releases and issue tracking.",
	Documentation:     "Documentation",
	DocumentationDesc: "Installation, configuration and usage guides.",
	QQGroup:           "QQ Group",
	QQGroupDesc:       "Community discussion, support and feedback.",
	OpenSource:        "AgentDock · Open Source",
	License:           "MIT License",
	DocumentationURL:  agentDockDocsURL,
}

var statusPageChinese = statusPageText{
	Lang:              "zh-CN",
	Subtitle:          "AI Agent 设备运行时",
	Online:            "在线",
	ReadyTitle:        "已准备好为 AI Agent 提供能力。",
	ReadyDescription:  "当前 AgentDock 实例在线，可通过 MCP 提供本机能力。",
	Version:           "版本",
	System:            "系统",
	Capabilities:      "能力",
	Tools:             "工具",
	Plugins:           "插件",
	MCPReady:          "就绪",
	Browser:           "浏览器",
	Auth:              "鉴权",
	Enabled:           "已启用",
	Disabled:          "未启用",
	None:              "无",
	Token:             "访问令牌",
	OAuthAndToken:     "OAuth + 访问令牌",
	MCPEndpoint:       "MCP 端点",
	Copy:              "复制",
	Copied:            "已复制",
	CopyFailed:        "复制失败",
	EndpointHint:      "使用此端点将 AgentDock 连接到 MCP 客户端。",
	Resources:         "资源",
	Repository:        "GitHub 仓库",
	RepositoryDesc:    "源代码、版本发布与问题反馈。",
	Documentation:     "文档",
	DocumentationDesc: "安装、配置与使用指南。",
	QQGroup:           "QQ 群",
	QQGroupDesc:       "社区交流、使用支持与反馈。",
	OpenSource:        "AgentDock · 开源",
	License:           "MIT 许可证",
	DocumentationURL:  agentDockDocsURL + "zh-CN/",
}

type statusPageData struct {
	Text             statusPageText
	Version          string
	Platform         string
	ToolCount        int
	PluginCount      string
	MCPEndpoint      string
	ACPEnabled       bool
	ACPStatus        string
	RecallEnabled    bool
	RecallStatus     string
	BrowserEnabled   bool
	BrowserStatus    string
	AuthEnabled      bool
	AuthStatus       string
	RepositoryURL    string
	DocumentationURL string
	QQGroup          string
	QQGroupURL       string
}

func statusPageHandler(server *mcp.Server, runtime runtimeapi.Runtime, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		text := preferredStatusPageText(r.Header.Get("Accept-Language"))
		build := buildinfo.Current()
		recallEnabled := strings.TrimSpace(cfg.NexusEndpoint) != ""
		authEnabled := cfg.AuthRequired()
		pluginCount := "—"
		if runtime != nil {
			plugins, pluginErr := runtime.RuntimePlugins(r.Context())
			if pluginErr != nil {
				slog.Warn("read Plugin count for status page failed", "error", pluginErr)
			} else if count, ok := plugins["count"].(int); ok {
				pluginCount = strconv.Itoa(count)
			}
		}
		data := statusPageData{
			Text:             text,
			Version:          build.Version,
			Platform:         build.Platform,
			ToolCount:        len(server.ToolNames()),
			PluginCount:      pluginCount,
			MCPEndpoint:      issuerFor(cfg, r) + "/mcp",
			ACPEnabled:       cfg.ACPEnabled,
			ACPStatus:        enabledLabel(text, cfg.ACPEnabled),
			RecallEnabled:    recallEnabled,
			RecallStatus:     enabledLabel(text, recallEnabled),
			BrowserEnabled:   cfg.BrowserEnabled,
			BrowserStatus:    enabledLabel(text, cfg.BrowserEnabled),
			AuthEnabled:      authEnabled,
			AuthStatus:       authLabel(text, cfg),
			RepositoryURL:    agentDockRepositoryURL,
			DocumentationURL: text.DocumentationURL,
			QQGroup:          agentDockQQGroup,
			QQGroupURL:       agentDockQQGroupURL,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Vary", "Accept-Language")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", statusPageCSP)
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		if err := statusPageTemplate.Execute(w, data); err != nil {
			slog.Warn("render status page failed", "error", err)
		}
	}
}

func preferredStatusPageText(header string) statusPageText {
	bestLanguage := "en"
	bestQuality := -1.0

	for _, raw := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(raw), ";")
		language := strings.ToLower(strings.TrimSpace(parts[0]))
		if language != "en" && !strings.HasPrefix(language, "en-") && language != "zh" && !strings.HasPrefix(language, "zh-") {
			continue
		}

		quality := 1.0
		for _, parameter := range parts[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
				continue
			}
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				quality = 0
			} else {
				quality = parsed
			}
			break
		}
		if quality <= 0 || quality <= bestQuality {
			continue
		}

		bestQuality = quality
		if language == "zh" || strings.HasPrefix(language, "zh-") {
			bestLanguage = "zh"
		} else {
			bestLanguage = "en"
		}
	}

	if bestLanguage == "zh" {
		return statusPageChinese
	}
	return statusPageEnglish
}

func enabledLabel(text statusPageText, enabled bool) string {
	if enabled {
		return text.Enabled
	}
	return text.Disabled
}

func authLabel(text statusPageText, cfg config.Config) string {
	hasToken := strings.TrimSpace(cfg.AuthToken) != ""
	if cfg.OAuthEnabled && hasToken {
		return text.OAuthAndToken
	}
	if cfg.OAuthEnabled {
		return "OAuth"
	}
	if hasToken {
		return text.Token
	}
	return text.None
}
