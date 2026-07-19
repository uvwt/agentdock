//go:build windows

package skillstate

import (
	"errors"

	"golang.org/x/sys/windows"
)

func isDirectoryBusy(err error) bool {
	return errors.Is(err, windows.ERROR_DIR_NOT_EMPTY) || errors.Is(err, windows.ERROR_ALREADY_EXISTS)
}

func isTransientLockContention(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
