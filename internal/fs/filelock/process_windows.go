//go:build windows

package filelock

import (
	"errors"
	"os"

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

// Windows 删除目录期间，竞争者创建同名目录可能短暂收到 Access Denied
// 或 Sharing Violation。这两种错误与“目录已存在”一样属于锁竞争，应该
// 继续等待；其他错误仍立即返回，避免把真实路径或权限错误隐藏成超时。
func retryableLockCreationError(err error) bool {
	return errors.Is(err, os.ErrExist) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
