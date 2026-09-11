//go:build windows

package processlock

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func tryAcquire(path string) (*Lock, bool, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, false, err
	}
	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("open exclusive process lock: %w", err)
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, false, errors.New("create process lock file handle")
	}
	return &Lock{file: file}, true, nil
}

func release(file *os.File) error {
	return file.Close()
}
