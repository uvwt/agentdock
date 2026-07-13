//go:build darwin || linux

package atomicfile

import "os"

func secureWrittenFile(_ string, _ os.FileMode) error {
	// 临时文件在写入和 fsync 前已经设置精确权限，rename 会原样保留权限。
	// 这里不能再套用统一的 0600 私有权限，否则会把 0700 脚本降级为不可执行。
	return nil
}
