package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatusPageRendersConnectionAndResourceLinks(t *testing.T) {
	cfg := testConfig(t)
	cfg.OAuthServerURL = "https://agentdock.example.com"
	cfg.OAuthEnabled = true
	cfg.ACPEnabled = true
	cfg.BrowserEnabled = true
	cfg.NexusEndpoint = "http://127.0.0.1:18777"

	response := httptest.NewRecorder()
	statusPageHandler(nil, nil, cfg).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	for header, expected := range map[string]string{
		"Cache-Control":           "no-store",
		"Content-Security-Policy": statusPageCSP,
		"Referrer-Policy":         "no-referrer",
		"Vary":                    "Accept-Language",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
	} {
		if got := response.Header().Get(header); got != expected {
			t.Fatalf("%s = %q, want %q", header, got, expected)
		}
	}
	body := response.Body.String()
	for _, expected := range []string{
		"AgentDock",
		"https://agentdock.example.com/mcp",
		"github.com/uvwt/agentdock",
		"uvwt.github.io/agentdock-docs",
		"1081337019",
		`class="state-enabled"`,
		`class="state-auth"`,
		`class="resource resource-documentation"`,
		">OAuth<",
		">Enabled<",
		">Plugins<",
		"navigator.clipboard.writeText",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("status page missing %q", expected)
		}
	}
}

func TestStatusPageUsesChineseForChineseBrowserLanguage(t *testing.T) {
	cfg := testConfig(t)
	request := httptest.NewRequest(http.MethodGet, "https://dock.example/", nil)
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	response := httptest.NewRecorder()

	statusPageHandler(nil, nil, cfg).ServeHTTP(response, request)

	body := response.Body.String()
	for _, expected := range []string{
		`<html lang="zh-CN">`,
		"AI Agent 设备运行时",
		"已准备好为 AI Agent 提供能力。",
		"MCP 端点",
		`data-copied="已复制"`,
		"GitHub 仓库",
		"安装、配置与使用指南。",
		">插件<",
		`href="https://uvwt.github.io/agentdock-docs/zh-CN/"`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("Chinese status page missing %q", expected)
		}
	}
}

func TestAuthLabelCoversConfiguredAuthModes(t *testing.T) {
	tests := []struct {
		name    string
		oauth   bool
		token   string
		english string
		chinese string
	}{
		{name: "none", english: "None", chinese: "无"},
		{name: "token", token: "secret", english: "Token", chinese: "访问令牌"},
		{name: "oauth", oauth: true, english: "OAuth", chinese: "OAuth"},
		{name: "oauth and token", oauth: true, token: "secret", english: "OAuth + Token", chinese: "OAuth + 访问令牌"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.OAuthEnabled = test.oauth
			cfg.AuthToken = test.token
			if got := authLabel(statusPageEnglish, cfg); got != test.english {
				t.Fatalf("English auth label = %q, want %q", got, test.english)
			}
			if got := authLabel(statusPageChinese, cfg); got != test.chinese {
				t.Fatalf("Chinese auth label = %q, want %q", got, test.chinese)
			}
		})
	}
}

func TestPreferredStatusPageTextRespectsLanguageQuality(t *testing.T) {
	tests := []struct {
		header string
		lang   string
	}{
		{header: "zh-CN,zh;q=0.9,en;q=0.8", lang: "zh-CN"},
		{header: "en-US,en;q=0.9,zh;q=0.8", lang: "en"},
		{header: "fr-FR,zh;q=0.8,en;q=0.7", lang: "zh-CN"},
		{header: "fr-FR", lang: "en"},
		{header: "zh;q=0,en;q=0.5", lang: "en"},
		{header: "en;q=0.4,zh;q=0.9", lang: "zh-CN"},
		{header: "zh;q=invalid,en;q=0.5", lang: "en"},
	}

	for _, test := range tests {
		t.Run(test.header, func(t *testing.T) {
			if got := preferredStatusPageText(test.header).Lang; got != test.lang {
				t.Fatalf("preferredStatusPageText(%q).Lang = %q, want %q", test.header, got, test.lang)
			}
		})
	}
}

func TestStatusPageUsesRequestOriginWithoutConfiguredPublicURL(t *testing.T) {
	cfg := testConfig(t)
	request := httptest.NewRequest(http.MethodGet, "https://dock.example/", nil)
	response := httptest.NewRecorder()

	statusPageHandler(nil, nil, cfg).ServeHTTP(response, request)

	if !strings.Contains(response.Body.String(), "https://dock.example/mcp") {
		t.Fatalf("status page endpoint = %s", response.Body.String())
	}
}

func TestStatusPageOnlyHandlesExactRoot(t *testing.T) {
	cfg := testConfig(t)
	response := httptest.NewRecorder()

	statusPageHandler(nil, nil, cfg).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestStatusPageRejectsUnsupportedMethods(t *testing.T) {
	cfg := testConfig(t)
	response := httptest.NewRecorder()

	statusPageHandler(nil, nil, cfg).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", nil))

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if got := response.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow = %q, want %q", got, "GET, HEAD")
	}
}
