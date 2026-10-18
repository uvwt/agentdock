//go:build !darwin && !linux && !windows

package desktopruntime

// DefaultRuntimeRoot 在未定义标准目录的平台上只接受显式环境覆盖。
func DefaultRuntimeRoot() string {
	return configuredRuntimeRoot()
}
