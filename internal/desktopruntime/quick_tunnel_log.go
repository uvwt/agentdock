package desktopruntime

import (
	"regexp"
	"strings"
)

const quickTunnelCreatedMarker = "Your quick Tunnel has been created! Visit it at"

var quickTunnelURLPattern = regexp.MustCompile(`https://[A-Za-z0-9-]+\.trycloudflare\.com`)

type quickTunnelLogParser struct {
	created bool
}

func (p *quickTunnelLogParser) URL(line string) string {
	// cloudflared 失败日志也会打印 provisioning API 地址；只有明确进入“Tunnel 已创建”阶段后，
	// 后续 trycloudflare.com 地址才代表可对外使用的临时 Tunnel。
	if !p.created {
		if !strings.Contains(line, quickTunnelCreatedMarker) {
			return ""
		}
		p.created = true
	}
	return quickTunnelURLPattern.FindString(line)
}

func findQuickTunnelURL(log []byte) string {
	parser := quickTunnelLogParser{}
	for _, line := range strings.Split(string(log), "\n") {
		if publicURL := parser.URL(line); publicURL != "" {
			return publicURL
		}
	}
	return ""
}
