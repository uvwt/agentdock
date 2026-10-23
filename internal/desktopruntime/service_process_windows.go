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

const (
	binaryStopPollInterval = 50 * time.Millisecond
	binaryStopQuietPeriod  = 500 * time.Millisecond
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

func stopBinaryProcessesExcept(ctx context.Context, binaryPath string, excluded map[uint32]struct{}, timeout time.Duration, otherBinaryPaths ...string) error {
	binaryPaths := append([]string{binaryPath}, otherBinaryPaths...)
	deadline := time.Now().Add(timeout)
	var quietSince time.Time
	sawProcess := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		scanStartedAt := time.Now()
		processes, err := processIDsAtPathsExcept(binaryPaths, excluded)
		if err != nil {
			return err
		}
		if len(processes) > 0 {
			// 不能只终止第一次快照。Tray、Task Scheduler 等外部宿主可能在等待窗口
			// 内再次拉起同一路径进程；每轮重新扫描并终止，直到路径稳定为空。
			sawProcess = true
			quietSince = time.Time{}
			if err := terminateProcessIDs(processes); err != nil {
				return err
			}
		} else if !sawProcess {
			return nil
		} else if quietSince.IsZero() {
			quietSince = time.Now()
		} else if scanStartedAt.Sub(quietSince) >= binaryStopQuietPeriod {
			// 快照采集早于逐进程路径查询；慢扫描不能把旧的空快照误算为
			// 最新状态，否则扫描期间刚拉起的替代进程会被漏掉。
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("进程未在 %s 内退出: %s", timeout, binaryPath)
		}
		wait := binaryStopPollInterval
		if remaining < wait {
			wait = remaining
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// WaitBinaryStopped waits until no process with the exact binary path remains.
func WaitBinaryStopped(ctx context.Context, binaryPath string, timeout time.Duration) (bool, error) {
	return waitBinaryStoppedExcept(ctx, binaryPath, nil, timeout)
}

func waitBinaryStoppedExcept(ctx context.Context, binaryPath string, excluded map[uint32]struct{}, timeout time.Duration, otherBinaryPaths ...string) (bool, error) {
	binaryPaths := append([]string{binaryPath}, otherBinaryPaths...)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		processes, err := processIDsAtPathsExcept(binaryPaths, excluded)
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
	return terminateProcessIDs(processIDs)
}

func terminateProcessIDs(processIDs []uint32) error {
	var failures []string
	for _, processID := range processIDs {
		process, openErr := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, processID)
		if openErr != nil {
			// 进程可能已经在快照与 OpenProcess 之间退出。
			if errors.Is(openErr, windows.ERROR_INVALID_PARAMETER) {
				continue
			}
			failures = append(failures, fmt.Sprintf("PID %d: %v", processID, openErr))
			continue
		}
		state, waitErr := windows.WaitForSingleObject(process, 0)
		if waitErr == nil && state == windows.WAIT_OBJECT_0 {
			_ = windows.CloseHandle(process)
			continue
		}
		terminateErr := windows.TerminateProcess(process, 0)
		state, waitErr = windows.WaitForSingleObject(process, 5000)
		_ = windows.CloseHandle(process)
		// Task Scheduler/进程自身可能同时退出，TerminateProcess 因此返回 Access Denied。
		// 只认同一个句柄的退出结果；仍存活的权限错误不能吞掉。
		if waitErr == nil && state == windows.WAIT_OBJECT_0 {
			continue
		}
		if terminateErr != nil {
			failures = append(failures, fmt.Sprintf("PID %d: %v", processID, terminateErr))
		} else if waitErr != nil {
			failures = append(failures, fmt.Sprintf("等待 PID %d 退出: %v", processID, waitErr))
		} else {
			failures = append(failures, fmt.Sprintf("PID %d 未退出", processID))
		}
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

func processIDsAtPathsExcept(binaryPaths []string, excluded map[uint32]struct{}) ([]uint32, error) {
	var processes []uint32
	for _, path := range binaryPaths {
		ids, err := processIDsAtPathExcept(path, excluded)
		if err != nil {
			return nil, err
		}
		processes = append(processes, ids...)
	}
	return processes, nil
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

// ancestorProcessIDsAtPath returns ancestor processes whose executable path exactly
// matches binaryPath. A live generation update runs as:
// stable shim -> source generation updater -> source Arbiter -> stable shim -> service stop.
// The stop helper must terminate the long-running source Core, but it must not kill the
// source updater that is synchronously waiting for the Arbiter's terminal result. Recovery
// Arbiters do not have that updater ancestor, so crash recovery still stops every source Core.
func ancestorProcessIDsAtPath(binaryPath string, otherBinaryPaths ...string) (map[uint32]struct{}, error) {
	targets := append([]string{binaryPath}, otherBinaryPaths...)
	for i, path := range targets {
		target, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		targets[i] = target
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("创建 Windows 进程快照失败: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	parents := make(map[uint32]uint32)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, fmt.Errorf("读取 Windows 进程快照失败: %w", err)
	}
	for {
		parents[entry.ProcessID] = entry.ParentProcessID
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, syscall.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, fmt.Errorf("继续读取 Windows 进程快照失败: %w", err)
		}
	}

	excluded := map[uint32]struct{}{}
	seen := map[uint32]struct{}{}
	processID := uint32(os.Getpid())
	for {
		parentID := parents[processID]
		if parentID == 0 {
			break
		}
		if _, ok := seen[parentID]; ok {
			break
		}
		seen[parentID] = struct{}{}
		if processPath, pathErr := queryProcessPath(parentID); pathErr == nil {
			for _, target := range targets {
				if samePath(processPath, target) {
					excluded[parentID] = struct{}{}
					break
				}
			}
		}
		processID = parentID
	}
	return excluded, nil
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
