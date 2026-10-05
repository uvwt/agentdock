//go:build darwin

package component

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func verifyPlatformTrust(ctx context.Context, path string, _ bool) error {
	// 下载 archive 的 pinned digest 只证明拿到的是受审计的上游字节；解包后仍要验证
	// 最终可执行文件的 macOS 代码签名，避免把 archive 完整性误当成平台执行信任。
	output, err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--strict", "--verbose=2", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify cloudflared macOS code signature: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
