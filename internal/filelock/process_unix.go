//go:build darwin || linux

package filelock

import (
	"errors"
	"syscall"
)

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true
	}
	return !errors.Is(err, syscall.ESRCH)
}
