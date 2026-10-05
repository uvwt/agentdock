//go:build darwin

package component

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func verifyPlatformTrust(ctx context.Context, path string, legacy bool) error {
	if !legacy {
		// Catalog SHA-256 是下载组件的发布信任边界。component 位于签名 App 外部，
		// 不能在安装后重新签名，否则同一版本会因本机签名产生不同 digest。
		return nil
	}
	output, err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--strict", "--verbose=2", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify legacy bundled cloudflared signature: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
