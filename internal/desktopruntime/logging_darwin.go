//go:build darwin

package desktopruntime

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/uvwt/agentdock/internal/logfile"
)

func platformOpenCoreLog(string) (io.WriteCloser, error) {
	logDir, err := macOSLogDir()
	if err != nil {
		return nil, err
	}
	return logfile.OpenDefault(filepath.Join(logDir, "agentdock.err.log"))
}

func platformOpenTunnelLogs(unixRuntimeManifest) (*processLogs, error) {
	logDir, err := macOSLogDir()
	if err != nil {
		return nil, err
	}
	return openProcessLogs(
		filepath.Join(logDir, "cloudflared.out.log"),
		filepath.Join(logDir, "cloudflared.err.log"),
	)
}

func macOSLogDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("解析 macOS 用户目录失败: %w", err)
	}
	return filepath.Join(home, "Library", "Logs", "AgentDock"), nil
}
