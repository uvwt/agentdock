package httpx

import (
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
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
}

func writeAuthorizeForm(w http.ResponseWriter, values url.Values, errorText string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")

	message := ""
	if errorText != "" {
		message = "密码不正确，请重试。"
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
	}
	if err := authorizePageTemplate.Execute(w, data); err != nil {
		slog.Warn("write OAuth authorization form failed", "error", err)
	}
}
