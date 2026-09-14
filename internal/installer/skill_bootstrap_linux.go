//go:build linux

package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
)

// tryBootstrapAsServiceUser 在 Engine 以 root 运行时（wrapper run_root），
// 以 service user 身份执行 skill bootstrap：skill state 属于运行时身份，
// root 落盘会让 service user 无法读写自己的状态目录。
// 返回 (handled, error)：handled=true 表示降权路径已完成（无论成败）；
// handled=false 表示当前不需要降权，调用方走进程内路径。
func tryBootstrapAsServiceUser(ctx context.Context, request Request, executable, home, bundleDir string) (bool, error) {
	if os.Geteuid() != 0 {
		return false, nil
	}
	username := strings.TrimSpace(request.ServiceUser)
	if username == "" {
		return false, nil
	}
	u, err := user.Lookup(username)
	if err != nil {
		return true, fmt.Errorf("解析 service user %s: %w", username, err)
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return true, fmt.Errorf("service user %s uid 无效: %w", username, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return true, fmt.Errorf("service user %s gid 无效: %w", username, err)
	}

	executable = strings.TrimSpace(executable)
	if executable == "" {
		return true, fmt.Errorf("skill bootstrap 缺少已安装 live binary")
	}
	// 必须 re-exec 已经切换到 install root 且显式 chmod 0755 的 live binary。
	// 当前 Engine 可能运行在安装用户的 0700 临时目录里；降权后再 exec 那个
	// 临时 payload 会因 service user 无法穿过目录而 EACCES。
	// 环境只保留运行 skill bootstrap 所需的最小集合，避免把 root 侧 secrets
	// 泄漏进降权子进程。
	cmd := exec.CommandContext(ctx, executable, "skill", "bootstrap", "--bundle", bundleDir)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"AGENTDOCK_HOME=" + home,
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return true, fmt.Errorf("skill bootstrap (as %s): %w: %s", username, err, strings.TrimSpace(string(output)))
	}
	return true, nil
}
