package selfupdate

import (
	"context"
	"time"
)

type managedService interface {
	Name() string
	Restart(context.Context) error
}

// managedServiceVerifier 只由能可靠核对托管进程的平台实现。
// healthz 单独成功不足以证明请求命中了刚重启的新进程，因此 macOS 会同时核对 PID、目标二进制和监听端口。
type managedServiceVerifier interface {
	CurrentPID(context.Context) (int, error)
	WaitForRestart(context.Context, string, int, time.Duration) error
}
