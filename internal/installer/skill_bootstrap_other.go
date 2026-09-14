//go:build !linux

package installer

import "context"

// 非 Linux 平台的安装全部以当前用户身份执行（macOS 登录用户 / Windows 交互用户），
// skill state 天然归属运行身份，不存在降权问题。
func tryBootstrapAsServiceUser(ctx context.Context, request Request, executable, home, bundleDir string) (bool, error) {
	return false, nil
}
