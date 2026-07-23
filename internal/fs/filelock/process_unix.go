//go:build darwin || linux

package filelock

import (
	"errors"
	"os"
	"syscall"
)

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true
	}
	return !errors.Is(err, syscall.ESRCH)
}

func retryableLockCreationError(err error) bool {
	return errors.Is(err, os.ErrExist)
}
