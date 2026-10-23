//go:build windows

package desktopcontrol

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func startPipeTestServer(t *testing.T, handler Handler) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, root, handler) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		callCtx, callCancel := context.WithTimeout(context.Background(), time.Second)
		err := Call(callCtx, root, "ping", nil, nil)
		callCancel()
		if err == nil {
			return root, cancel, done
		}
		if time.Now().After(deadline) {
			t.Fatalf("control server not ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStalledPipeClientDoesNotBlockControlOrShutdown(t *testing.T) {
	t.Run("incomplete request", func(t *testing.T) { testStalledPipeClient(t, false) })
	t.Run("unread response", func(t *testing.T) { testStalledPipeClient(t, true) })
}

func testStalledPipeClient(t *testing.T, unreadResponse bool) {
	root, cancel, done := startPipeTestServer(t, func(context.Context, Request) (any, error) {
		return strings.Repeat("x", maxMessageBytes/2), nil
	})
	name, err := windows.UTF16PtrFromString(endpointPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var pipe windows.Handle
	deadline := time.Now().Add(time.Second)
	for {
		pipe, err = windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer windows.CloseHandle(pipe)
	// 模拟控制面板中途挂起：请求只写一部分，或发送完整请求却不读取响应。
	if unreadResponse {
		if err := writePipeMessage(pipe, []byte(`{"id":"stalled","method":"ping"}`)); err != nil {
			t.Fatal(err)
		}
	} else {
		var written uint32
		if err := windows.WriteFile(pipe, []byte{1}, &written, nil); err != nil {
			t.Fatal(err)
		}
	}
	result := make(chan error, 1)
	go func() {
		ctx, stop := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer stop()
		result <- Call(ctx, root, "ping", nil, nil)
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Errorf("stalled client blocked next request: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("next control request hung")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(time.Second):
		t.Error("stalled client blocked server shutdown")
	}
}

func TestPipeCallCancelsWhileWaitingForResponse(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	entered := make(chan struct{})
	root, _, _ := startPipeTestServer(t, func(ctx context.Context, request Request) (any, error) {
		if request.Method == "wait" {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- Call(ctx, root, "wait", nil, nil) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler not called")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Error("pipe response read ignored cancellation")
	}
}
