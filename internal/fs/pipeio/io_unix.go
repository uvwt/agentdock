//go:build !windows

package pipeio

import (
	"context"
	"io"
)

// Write 在取消时关闭流以解除 Unix 管道写入；部分报文之后连接不能继续复用。
func Write(ctx context.Context, writer io.WriteCloser, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = writer.Close()
		close(interrupted)
	})
	written, err := writer.Write(data)
	if !stop() {
		<-interrupted
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil && written != len(data) {
		return io.ErrShortWrite
	}
	return err
}
