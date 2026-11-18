package httpx

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/uvwt/agentdock/internal/config"
)

// analyticsProxyHeaders 是 Tunnel/反向代理常见的转发特征。
// 未启用认证时，所有 Runtime API 只允许本机直连；这些 Header 只用于拒绝代理流量，从不作为放行依据。
var analyticsProxyHeaders = []string{
	"Forwarded",
	"X-Forwarded-For",
	"X-Forwarded-Host",
	"X-Forwarded-Proto",
	"X-Real-IP",
	"CF-Connecting-IP",
	"CF-Ray",
	"CF-Visitor",
}

func isDirectLoopbackRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	remote := parseRemoteIP(r.RemoteAddr)
	if remote == nil || !remote.IsLoopback() {
		return false
	}
	if !loopbackRequestHost(r.Host) {
		return false
	}
	if hasRuntimeProxyHeaders(r) {
		return false
	}
	return allowsRuntimeBrowserOrigin(r)
}

func validateRuntimeRequestOriginAndHost(r *http.Request, cfg config.Config) bool {
	if r == nil {
		return false
	}
	// 与 /mcp（go-sdk StreamableHTTPHandler LocalhostProtection）保持一致：
	// 当客户端通过本地回环连接接入、且未显式配置公网 URL 时，只接受回环 Host，
	// 阻断本地浏览器通过恶意域名的 DNS rebinding 攻击（即便携带了凭证）；
	// 若监听在非回环网络或远端直连（remote IP 非 loopback），则允许远程 Host。
	remote := parseRemoteIP(r.RemoteAddr)
	if (remote == nil || remote.IsLoopback()) && strings.TrimSpace(cfg.OAuthServerURL) == "" && !loopbackRequestHost(r.Host) {
		return false
	}
	return allowsRuntimeBrowserOrigin(r)
}

func hasRuntimeProxyHeaders(r *http.Request) bool {
	for _, header := range analyticsProxyHeaders {
		if strings.TrimSpace(r.Header.Get(header)) != "" {
			return true
		}
	}
	return false
}

func allowsRuntimeBrowserOrigin(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil ||
		!strings.EqualFold(parsed.Host, r.Host) || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	// 反向代理/Tunnel 终止 TLS 后转发给本机 HTTP 服务时，r.TLS 为空，
	// 需尊重可信代理头或 X-Forwarded-Proto 才能正确校验同源 HTTPS Origin；
	// 在未认证分支已提前拦截任何代理头，此处仅用于已认证同源检查。
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(firstHeaderToken(r.Header.Get("X-Forwarded-Proto")), "https") {
		scheme = "https"
	}
	return parsed.Scheme == scheme
}

func firstHeaderToken(value string) string {
	if value == "" {
		return ""
	}
	if idx := strings.IndexByte(value, ','); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimSpace(value)
}

func loopbackRequestHost(hostport string) bool {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return false
	}
	host := hostport
	if parsedHost, _, err := net.SplitHostPort(hostport); err == nil {
		host = parsedHost
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
