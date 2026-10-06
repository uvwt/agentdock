//go:build linux

package component

import (
	"context"
	"errors"
)

func verifyPlatformTrust(_ context.Context, _ string, legacy bool) error {
	if legacy {
		// Linux 上游没有可依赖的系统代码签名。旧路径没有 catalog digest，不能仅凭
		// --version 就把任意本地可执行文件提升为受管组件；标准安装应重新下载 pinned artifact。
		return errors.New("legacy cloudflared import is not trusted on Linux; install the pinned catalog artifact")
	}
	// 正常安装在进入此处前已经校验 pinned upstream SHA-256，随后还会执行 --version
	// 并与 catalog version 对比；Linux 没有额外的系统代码签名可验证。
	return nil
}
