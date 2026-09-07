//go:build windows

package desktopruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const tunnelSupervisorPIDFile = "tunnel-supervisor.pid"

type tunnelSupervisorGuard struct {
	mutex     windows.Handle
	stopEvent windows.Handle
	pidPath   string
}

func acquireTunnelSupervisor(runtimeRoot string) (*tunnelSupervisorGuard, error) {
	mutexName, err := windows.UTF16PtrFromString(tunnelSupervisorObjectName("mutex", runtimeRoot))
	if err != nil {
		return nil, err
	}
	mutex, createErr := windows.CreateMutex(nil, true, mutexName)
	if mutex == 0 {
		return nil, fmt.Errorf("创建 Tunnel supervisor mutex 失败: %w", createErr)
	}
	if errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		result, waitErr := windows.WaitForSingleObject(mutex, 0)
		if waitErr != nil {
			_ = windows.CloseHandle(mutex)
			return nil, fmt.Errorf("检查 Tunnel supervisor mutex 失败: %w", waitErr)
		}
		if result == uint32(windows.WAIT_TIMEOUT) {
			_ = windows.CloseHandle(mutex)
			return nil, nil
		}
		if result != uint32(windows.WAIT_OBJECT_0) && result != uint32(windows.WAIT_ABANDONED) {
			_ = windows.CloseHandle(mutex)
			return nil, fmt.Errorf("检查 Tunnel supervisor mutex 返回未知状态: 0x%x", result)
		}
	}

	stopEventName, err := windows.UTF16PtrFromString(tunnelSupervisorObjectName("stop", runtimeRoot))
	if err != nil {
		_ = windows.ReleaseMutex(mutex)
		_ = windows.CloseHandle(mutex)
		return nil, err
	}
	stopEvent, eventErr := windows.CreateEvent(nil, 1, 0, stopEventName)
	if stopEvent == 0 {
		_ = windows.ReleaseMutex(mutex)
		_ = windows.CloseHandle(mutex)
		return nil, fmt.Errorf("创建 Tunnel supervisor stop event 失败: %w", eventErr)
	}
	// 上一代 supervisor 在退出前可能已收到 stop；只有持有 mutex 的新一代可以清掉旧信号。
	if err := windows.ResetEvent(stopEvent); err != nil {
		_ = windows.CloseHandle(stopEvent)
		_ = windows.ReleaseMutex(mutex)
		_ = windows.CloseHandle(mutex)
		return nil, fmt.Errorf("重置 Tunnel supervisor stop event 失败: %w", err)
	}

	pidPath := filepath.Join(runtimeRoot, tunnelSupervisorPIDFile)
	if err := writeRuntimeText(pidPath, strconv.Itoa(os.Getpid())); err != nil {
		_ = windows.CloseHandle(stopEvent)
		_ = windows.ReleaseMutex(mutex)
		_ = windows.CloseHandle(mutex)
		return nil, err
	}
	return &tunnelSupervisorGuard{mutex: mutex, stopEvent: stopEvent, pidPath: pidPath}, nil
}

func (guard *tunnelSupervisorGuard) Close() {
	if guard == nil {
		return
	}
	_ = os.Remove(guard.pidPath)
	if guard.stopEvent != 0 {
		_ = windows.CloseHandle(guard.stopEvent)
	}
	if guard.mutex != 0 {
		_ = windows.ReleaseMutex(guard.mutex)
		_ = windows.CloseHandle(guard.mutex)
	}
}

func (guard *tunnelSupervisorGuard) stopRequested() (bool, error) {
	result, err := windows.WaitForSingleObject(guard.stopEvent, 0)
	if err != nil {
		return false, err
	}
	switch result {
	case uint32(windows.WAIT_OBJECT_0):
		return true, nil
	case uint32(windows.WAIT_TIMEOUT):
		return false, nil
	default:
		return false, fmt.Errorf("等待 Tunnel supervisor stop event 返回未知状态: 0x%x", result)
	}
}

func (guard *tunnelSupervisorGuard) waitRetry(ctx context.Context, delay time.Duration) (bool, error) {
	deadline := time.Now().Add(delay)
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, nil
		}
		wait := remaining
		if wait > 500*time.Millisecond {
			wait = 500 * time.Millisecond
		}
		result, err := windows.WaitForSingleObject(guard.stopEvent, uint32(wait.Milliseconds()))
		if err != nil {
			return false, err
		}
		switch result {
		case uint32(windows.WAIT_OBJECT_0):
			return true, nil
		case uint32(windows.WAIT_TIMEOUT):
		default:
			return false, fmt.Errorf("等待 Tunnel supervisor 重试返回未知状态: 0x%x", result)
		}
	}
}

func signalTunnelSupervisorStop(runtimeRoot string) error {
	name, err := windows.UTF16PtrFromString(tunnelSupervisorObjectName("stop", runtimeRoot))
	if err != nil {
		return err
	}
	event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("打开 Tunnel supervisor stop event 失败: %w", err)
	}
	defer windows.CloseHandle(event)
	if err := windows.SetEvent(event); err != nil {
		return fmt.Errorf("发送 Tunnel supervisor stop 信号失败: %w", err)
	}
	return nil
}

func waitTunnelSupervisorStopped(ctx context.Context, runtimeRoot string, timeout time.Duration) error {
	name, err := windows.UTF16PtrFromString(tunnelSupervisorObjectName("mutex", runtimeRoot))
	if err != nil {
		return err
	}
	mutex, err := windows.OpenMutex(windows.SYNCHRONIZE|windows.MUTEX_MODIFY_STATE, false, name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("打开 Tunnel supervisor mutex 失败: %w", err)
	}
	defer windows.CloseHandle(mutex)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		result, waitErr := windows.WaitForSingleObject(mutex, 250)
		if waitErr != nil {
			return fmt.Errorf("等待 Tunnel supervisor 停止失败: %w", waitErr)
		}
		switch result {
		case uint32(windows.WAIT_OBJECT_0), uint32(windows.WAIT_ABANDONED):
			_ = windows.ReleaseMutex(mutex)
			return nil
		case uint32(windows.WAIT_TIMEOUT):
		default:
			return fmt.Errorf("等待 Tunnel supervisor 停止返回未知状态: 0x%x", result)
		}
	}
	return fmt.Errorf("Tunnel supervisor 未在 %s 内退出", timeout)
}

func activeTunnelSupervisorPID(runtimeRoot, binaryPath string) (uint32, error) {
	name, err := windows.UTF16PtrFromString(tunnelSupervisorObjectName("mutex", runtimeRoot))
	if err != nil {
		return 0, err
	}
	mutex, err := windows.OpenMutex(windows.SYNCHRONIZE|windows.MUTEX_MODIFY_STATE, false, name)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("打开 Tunnel supervisor mutex 失败: %w", err)
	}
	defer windows.CloseHandle(mutex)

	result, waitErr := windows.WaitForSingleObject(mutex, 0)
	if waitErr != nil {
		return 0, fmt.Errorf("检查 Tunnel supervisor mutex 失败: %w", waitErr)
	}
	if result == uint32(windows.WAIT_OBJECT_0) || result == uint32(windows.WAIT_ABANDONED) {
		_ = windows.ReleaseMutex(mutex)
		return 0, nil
	}
	if result != uint32(windows.WAIT_TIMEOUT) {
		return 0, fmt.Errorf("检查 Tunnel supervisor mutex 返回未知状态: 0x%x", result)
	}

	data, err := os.ReadFile(filepath.Join(runtimeRoot, tunnelSupervisorPIDFile))
	if err != nil {
		return 0, fmt.Errorf("读取 Tunnel supervisor PID 失败: %w", err)
	}
	pid64, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
	if err != nil || pid64 == 0 {
		return 0, fmt.Errorf("Tunnel supervisor PID 无效: %q", strings.TrimSpace(string(data)))
	}
	pid := uint32(pid64)
	processPath, err := queryProcessPath(pid)
	if err != nil {
		return 0, fmt.Errorf("读取 Tunnel supervisor 进程路径失败: %w", err)
	}
	if !samePath(processPath, binaryPath) {
		return 0, fmt.Errorf("Tunnel supervisor PID %d 指向意外程序: %s", pid, processPath)
	}
	return pid, nil
}

func tunnelSupervisorObjectName(kind, runtimeRoot string) string {
	root, err := filepath.Abs(runtimeRoot)
	if err != nil {
		root = runtimeRoot
	}
	normalized := strings.ToLower(filepath.Clean(root))
	sum := sha256.Sum256([]byte(normalized))
	return "Local\\AgentDock.Tunnel." + kind + "." + hex.EncodeToString(sum[:12])
}
