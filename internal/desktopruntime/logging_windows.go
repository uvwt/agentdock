//go:build windows

package desktopruntime

import (
	"io"
	"path/filepath"

	"github.com/uvwt/agentdock/internal/logfile"
)

func platformOpenCoreLog(runtimeRoot string) (io.WriteCloser, error) {
	_, root, err := loadDesktopManifest(runtimeRoot)
	if err != nil {
		return nil, err
	}
	return logfile.OpenDefault(filepath.Join(root, "logs", "agentdock.err.log"))
}
