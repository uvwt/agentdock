//go:build windows

package filelock

import (
	"errors"

	"golang.org/x/sys/windows"
)

const stillActiveExitCode = 259

func processAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return false
		}
		// 未知查询失败按存活处理，宁可保留陈旧锁，也不能抢占仍在运行的进程。
		return true
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return true
	}
	return exitCode == stillActiveExitCode
}
