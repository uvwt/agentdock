//go:build !windows

package desktopcontrol

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

func endpointPath(runtimeRoot string) string {
	return filepath.Join(runtimeRoot, "control.sock")
}

func servePlatform(ctx context.Context, runtimeRoot string, handle func(context.Context, []byte) []byte) error {
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		return fmt.Errorf("create desktop control directory: %w", err)
	}
	path := endpointPath(runtimeRoot)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("desktop control endpoint is not a socket: %s", path)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove stale desktop control socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect desktop control socket: %w", err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("listen desktop control socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(path)
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure desktop control socket: %w", err)
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept desktop control connection: %w", acceptErr)
		}
		go serveConnection(ctx, connection, handle)
	}
}

func serveConnection(ctx context.Context, connection net.Conn, handle func(context.Context, []byte) []byte) {
	defer connection.Close()
	ctx, cancel := context.WithTimeout(ctx, controlRequestTimeout)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	_ = connection.SetDeadline(deadline)
	data, err := io.ReadAll(io.LimitReader(connection, maxMessageBytes+1))
	if err != nil || len(data) > maxMessageBytes {
		return
	}
	_, _ = connection.Write(handle(ctx, data))
}

func callPlatform(ctx context.Context, runtimeRoot string, data []byte) ([]byte, error) {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	connection, err := dialer.DialContext(ctx, "unix", endpointPath(runtimeRoot))
	if err != nil {
		return nil, fmt.Errorf("connect desktop control socket: %w", err)
	}
	defer connection.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(controlRequestTimeout)
	}
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	_ = connection.SetDeadline(deadline)
	if _, err := connection.Write(data); err != nil {
		return nil, fmt.Errorf("write desktop control request: %w", err)
	}
	if unixConnection, ok := connection.(*net.UnixConn); ok {
		_ = unixConnection.CloseWrite()
	}
	responseData, err := io.ReadAll(io.LimitReader(bufio.NewReader(connection), maxMessageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read desktop control response: %w", err)
	}
	if len(responseData) > maxMessageBytes {
		return nil, errors.New("desktop control response is too large")
	}
	return responseData, nil
}
