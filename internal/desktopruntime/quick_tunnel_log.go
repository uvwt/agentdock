package desktopruntime

import (
	"errors"
	"io"
	"os"
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

type quickTunnelLogCursor struct {
	size int64
}

func captureQuickTunnelLogCursor(path string) (quickTunnelLogCursor, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return quickTunnelLogCursor{}, nil
	}
	if err != nil {
		return quickTunnelLogCursor{}, err
	}
	return quickTunnelLogCursor{size: info.Size()}, nil
}

func readQuickTunnelLogSince(path string, cursor quickTunnelLogCursor) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	offset := cursor.size
	// 轮转或截断后 active log 会比启动前更短，此时新一代日志从文件开头读取。
	if info.Size() < offset {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(file)
}
