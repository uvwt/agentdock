package httpx

import (
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/uvwt/agentdock/internal/auth"
)

// 授权页必须保持完全自包含，避免 OAuth 流程依赖第三方静态资源或前端构建链路。
//
//go:embed oauth_authorize_page.html
var authorizePageHTML string

var authorizePageTemplate = template.Must(template.New("oauth-authorize").Parse(authorizePageHTML))

type authorizePageData struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	Resource            string
	State               string
	Error               string
	ClientName          string
	ClientInitial       string
	RedirectHost        string
	CancelURL           string
}

func authorizationFormCSP(redirectURI string) string {
	formAction := "'self'"
	parsed, err := url.Parse(redirectURI)
	if err == nil && validOAuthRedirectURI(redirectURI) {
		// 浏览器会把表单提交后的 302 回调也纳入 form-action 校验，因此只放行已验证 redirect_uri 的来源。
		formAction += " " + parsed.Scheme + "://" + parsed.Host
	}
	return "default-src 'none'; style-src 'unsafe-inline'; form-action " + formAction + "; base-uri 'none'; frame-ancestors 'none'"
}

func writeAuthorizeForm(w http.ResponseWriter, values url.Values, errorText, clientName string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("Content-Security-Policy", authorizationFormCSP(values.Get("redirect_uri")))

	message := ""
	if errorText != "" {
		message = "密码不正确，请重试。"
	}
	redirectHost := "已注册的应用"
	if parsed, err := url.Parse(values.Get("redirect_uri")); err == nil && parsed.Host != "" {
		redirectHost = parsed.Host
	}
	clientName = strings.TrimSpace(clientName)
	if clientName == "" {
		clientName = "未命名应用"
	}
	clientInitial := "?"
	if runes := []rune(clientName); len(runes) > 0 {
		clientInitial = string(runes[0])
	}
	cancelValues := url.Values{"error": {"access_denied"}}
	if state := values.Get("state"); state != "" {
		cancelValues.Set("state", state)
	}
	data := authorizePageData{
		ResponseType:        values.Get("response_type"),
		ClientID:            values.Get("client_id"),
		RedirectURI:         values.Get("redirect_uri"),
		CodeChallenge:       values.Get("code_challenge"),
		CodeChallengeMethod: values.Get("code_challenge_method"),
		Resource:            values.Get("resource"),
		State:               values.Get("state"),
		Error:               message,
		ClientName:          clientName,
		ClientInitial:       clientInitial,
		RedirectHost:        redirectHost,
		CancelURL:           auth.AppendQuery(values.Get("redirect_uri"), cancelValues),
	}
	if err := authorizePageTemplate.Execute(w, data); err != nil {
		slog.Warn("write OAuth authorization form failed", "error", err)
	}
}
