//go:build darwin || linux

package file

import (
	"fmt"
	"os"
	"syscall"
)

func platformFileIdentity(_ string, info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("read Unix file identity for %s", info.Name())
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
