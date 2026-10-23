//go:build windows

package pipeio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

var cancelSynchronousIOProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("CancelSynchronousIo")

// Write 等待取消的 Windows 同步写入真正退出后才返回，避免遗留占用管道的线程。
func Write(ctx context.Context, writer io.WriteCloser, data []byte) error {
	return Run(ctx, func() error {
		written, err := writer.Write(data)
		if err == nil && written != len(data) {
			return io.ErrShortWrite
		}
		return err
	})
}

// Run 让同步管道 I/O 服从上下文取消，包括连接、读取、写入和 flush。
func Run(ctx context.Context, operation func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	type connectorState struct {
		thread windows.Handle
		err    error
	}

	ready := make(chan connectorState, 1)
	connected := make(chan error, 1)
	released := make(chan struct{})
	defer close(released)
	go func() {
		// CancelSynchronousIo 只会取消指定线程发起的同步 I/O。
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		// 调用方停止 CancelSynchronousIo 后才释放线程，避免误取消复用该线程的其他 I/O。
		defer func() { <-released }()

		thread, err := windows.OpenThread(windows.THREAD_TERMINATE, false, windows.GetCurrentThreadId())
		ready <- connectorState{thread: thread, err: err}
		if err != nil {
			connected <- err
			return
		}

		if err := ctx.Err(); err != nil {
			connected <- err
			return
		}
		connected <- operation()
	}()

	connector := <-ready
	if connector.err != nil {
		return fmt.Errorf("open synchronous I/O thread: %w", connector.err)
	}
	defer windows.CloseHandle(connector.thread)

	select {
	case connectErr := <-connected:
		return connectErr
	case <-ctx.Done():
		// 取消可能先于实际 I/O 发起（ERROR_NOT_FOUND），也可能落在两次 I/O 之间。
		// 持续取消直到工作线程退出，之后调用方才可以安全关闭管道句柄。
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			_ = cancelSynchronousIO(connector.thread)
			select {
			case <-connected:
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

func cancelSynchronousIO(thread windows.Handle) error {
	result, _, callErr := cancelSynchronousIOProc.Call(uintptr(thread))
	if result != 0 {
		return nil
	}
	if callErr == syscall.Errno(0) {
		return errors.New("CancelSynchronousIo failed without an error code")
	}
	return callErr
}
