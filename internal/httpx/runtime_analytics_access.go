package httpx

import (
	"net"
	"net/http"
	"strings"
)

// analyticsProxyHeaders 是 Tunnel/反向代理常见的转发特征。
// 未启用认证时，Runtime Analytics 只允许本机直连；这些 Header 只用于拒绝代理流量，从不作为放行依据。
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
