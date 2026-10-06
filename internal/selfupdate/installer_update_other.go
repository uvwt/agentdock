//go:build !windows

package selfupdate

import (
	"context"
	"errors"
)

func applyPlatformInstallerUpdate(context.Context, installerApplyRequest) (applyResult, error) {
	return applyResult{}, errors.New("当前平台不支持完整安装器接管更新")
}
