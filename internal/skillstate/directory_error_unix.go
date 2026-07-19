//go:build darwin || linux

package skillstate

import (
	"errors"
	"syscall"
)

func isDirectoryBusy(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST)
}

func isTransientLockContention(_ error) bool {
	return false
}
