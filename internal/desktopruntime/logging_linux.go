//go:build linux

package desktopruntime

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/uvwt/agentdock/internal/logfile"
)

func platformOpenCoreLog(runtimeRoot string) (io.WriteCloser, error) {
	manifest, _, err := loadUnixRuntime(runtimeRoot)
	if err != nil {
		return nil, err
	}
	if manifest.ServiceManager != "openrc" {
		// systemd 继续交给 journald 管理日志容量和保留策略。
		return nil, nil
	}
	path, err := openRCLogPath(manifest.ServiceName, "agentdock.err.log")
	if err != nil {
		return nil, err
	}
	return logfile.OpenDefault(path)
}

func platformOpenTunnelLogs(manifest unixRuntimeManifest) (*processLogs, error) {
	if manifest.ServiceManager != "openrc" {
		return nil, nil
	}
	stdoutPath, err := openRCLogPath(manifest.TunnelServiceName, "cloudflared.out.log")
	if err != nil {
		return nil, err
	}
	stderrPath, err := openRCLogPath(manifest.TunnelServiceName, "cloudflared.err.log")
	if err != nil {
		return nil, err
	}
	return openProcessLogs(stdoutPath, stderrPath)
}

func openRCLogPath(serviceName, fileName string) (string, error) {
	if serviceName == "" || filepath.Base(serviceName) != serviceName {
		return "", fmt.Errorf("OpenRC 服务名无效: %q", serviceName)
	}
	if fileName == "" || filepath.Base(fileName) != fileName {
		return "", fmt.Errorf("OpenRC 日志文件名无效: %q", fileName)
	}
	return filepath.Join("/var/log", serviceName, fileName), nil
}
