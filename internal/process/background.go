package process

import (
	"os/exec"
	"runtime"
)

// ConfigureBackground 只为 Windows 的通用后台调用补 no-console 属性，
// 不改变 Darwin/Linux 现有的进程组语义。
func ConfigureBackground(cmd *exec.Cmd) {
	if runtime.GOOS != "windows" {
		return
	}
	Configure(cmd)
}
