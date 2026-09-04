//go:build !windows && !darwin && !linux

package desktopruntime

import "io"

func platformOpenCoreLog(string) (io.WriteCloser, error) {
	return nil, errServiceControlUnsupported
}
