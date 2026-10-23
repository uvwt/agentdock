package httpx

import (
	"net"
	"net/http"
	"net/url"
	"strings"
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
	for _, header := range analyticsProxyHeaders {
		if strings.TrimSpace(r.Header.Get(header)) != "" {
			return false
		}
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil ||
			!strings.EqualFold(parsed.Host, r.Host) || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return false
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if parsed.Scheme != scheme {
			return false
		}
	}
	return true
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
