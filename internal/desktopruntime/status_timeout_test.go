package desktopruntime

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/desktopcontrol"
)

func TestStatusCommandFallsBackWhenControlHandlerStalls(t *testing.T) {
	for _, command := range []struct {
		name string
		run  func(context.Context, []string, io.Writer, io.Writer) error
	}{
		{"service", RunServiceCommand}, {"tunnel", RunTunnelCommand},
	} {
		t.Run(command.name, func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() {
				done <- desktopcontrol.Serve(ctx, root, func(requestCtx context.Context, request desktopcontrol.Request) (any, error) {
					if request.Method != "ping" {
						<-requestCtx.Done()
					}
					return nil, nil
				})
			}()
			defer func() {
				cancel()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("control server did not stop")
				}
			}()
			deadline := time.Now().Add(3 * time.Second)
			for {
				if desktopcontrol.Call(ctx, root, "ping", nil, nil) == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("control server not ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			result := make(chan error, 1)
			go func() { result <- command.run(ctx, []string{"status", "--runtime-root", root}, io.Discard, io.Discard) }()
			select {
			case <-result:
				// 临时目录的系统查询可能返回空状态或配置错误，取决于平台；均应及时结束。
			case <-time.After(4 * time.Second):
				t.Error("status did not fall back from stalled control handler")
			}
		})
	}
}
