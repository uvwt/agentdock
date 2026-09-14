//go:build !windows

package installer

import (
	"context"
	"path/filepath"
)

func cloudflaredProcessRunningAtPath(ctx context.Context, path string) bool {
	base := filepath.Base(path)
	if path != base {
		// 有绝对路径时不要退回 pgrep -x 进程名，避免命中别人的 cloudflared。
		return cmdOK(ctx, "pgrep", "-f", path)
	}
	return cmdOK(ctx, "pgrep", "-x", base)
}
