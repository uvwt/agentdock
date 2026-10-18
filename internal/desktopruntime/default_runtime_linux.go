//go:build linux

package desktopruntime

// DefaultRuntimeRoot 返回官方 Linux 安装器的标准运行配置目录。
// 自定义安装可通过 AGENTDOCK_RUNTIME_ROOT 或命令行 --runtime-root 覆盖。
func DefaultRuntimeRoot() string {
	if root := configuredRuntimeRoot(); root != "" {
		return root
	}
	return "/etc/agentdock"
}
