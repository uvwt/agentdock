//go:build windows

package desktopruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// BinaryProcessRunning 只按完整可执行文件路径判断进程是否正在运行。
func BinaryProcessRunning(binaryPath string) (bool, error) {
	processes, err := processIDsAtPath(binaryPath)
	if err != nil {
		return false, err
	}
	return len(processes) > 0, nil
}

func processRunningAtPath(binaryPath string) (bool, error) {
	return BinaryProcessRunning(binaryPath)
}

// StopBinaryProcesses 只终止可执行文件路径与 binaryPath 完全一致的进程。
func StopBinaryProcesses(ctx context.Context, binaryPath string, timeout time.Duration) error {
	return stopBinaryProcessesExcept(ctx, binaryPath, nil, timeout)
}

func stopBinaryProcessesExcept(ctx context.Context, binaryPath string, excluded map[uint32]struct{}, timeout time.Duration) error {
	if err := terminateProcessesAtPathExcept(binaryPath, excluded); err != nil {
		return err
	}
	stopped, err := waitBinaryStoppedExcept(ctx, binaryPath, excluded, timeout)
	if err != nil {
		return err
	}
	if !stopped {
		return fmt.Errorf("进程未在 %s 内退出: %s", timeout, binaryPath)
	}
	return nil
}

// WaitBinaryStopped waits until no process with the exact binary path remains.
func WaitBinaryStopped(ctx context.Context, binaryPath string, timeout time.Duration) (bool, error) {
	return waitBinaryStoppedExcept(ctx, binaryPath, nil, timeout)
}

func waitBinaryStoppedExcept(ctx context.Context, binaryPath string, excluded map[uint32]struct{}, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		processes, err := processIDsAtPathExcept(binaryPath, excluded)
		if err != nil {
			return false, err
		}
		if len(processes) == 0 {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return false, nil
}

func waitBinaryStopped(ctx context.Context, binaryPath string, timeout time.Duration) (bool, error) {
	return WaitBinaryStopped(ctx, binaryPath, timeout)
}

func terminateProcessesAtPath(binaryPath string) error {
	return terminateProcessesAtPathExcept(binaryPath, nil)
}

func terminateProcessesAtPathExcept(binaryPath string, excluded map[uint32]struct{}) error {
	processIDs, err := processIDsAtPathExcept(binaryPath, excluded)
	if err != nil {
		return err
	}
	var failures []string
	for _, processID := range processIDs {
		process, openErr := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, processID)
		if openErr != nil {
			failures = append(failures, fmt.Sprintf("PID %d: %v", processID, openErr))
			continue
		}
		if terminateErr := windows.TerminateProcess(process, 0); terminateErr != nil {
			failures = append(failures, fmt.Sprintf("PID %d: %v", processID, terminateErr))
		}
		_, _ = windows.WaitForSingleObject(process, 5000)
		_ = windows.CloseHandle(process)
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func processIDsAtPathExcept(binaryPath string, excluded map[uint32]struct{}) ([]uint32, error) {
	processIDs, err := processIDsAtPath(binaryPath)
	if err != nil {
		return nil, err
	}
	if len(excluded) == 0 {
		return processIDs, nil
	}
	filtered := processIDs[:0]
	for _, processID := range processIDs {
		if _, keep := excluded[processID]; keep {
			continue
		}
		filtered = append(filtered, processID)
	}
	return filtered, nil
}

func processIDsAtPath(binaryPath string) ([]uint32, error) {
	target, err := filepath.Abs(binaryPath)
	if err != nil {
		return nil, err
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("创建 Windows 进程快照失败: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, fmt.Errorf("读取 Windows 进程快照失败: %w", err)
	}
	currentPID := uint32(os.Getpid())
	var processIDs []uint32
	for {
		if entry.ProcessID != currentPID {
			if processPath, pathErr := queryProcessPath(entry.ProcessID); pathErr == nil && samePath(processPath, target) {
				processIDs = append(processIDs, entry.ProcessID)
			}
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, syscall.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, fmt.Errorf("继续读取 Windows 进程快照失败: %w", err)
		}
	}
	return processIDs, nil
}

func queryProcessPath(processID uint32) (string, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(process)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buffer[:size]), nil
}
