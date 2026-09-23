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
func tryBootstrapAsServiceUser(ctx context.Context, request Request, executable, home, bundleDir string) (bool, error) {
	return runSkillCommandAsServiceUser(
		ctx,
		request,
		executable,
		home,
		"skill bootstrap",
		"skill", "bootstrap", "--bundle", bundleDir,
	)
}

// tryFinalizeSkillMigrationAsServiceUser 与 bootstrap 使用同一运行身份。
// Finalize 会在 ~/.agentdock/migrations 下创建备份目录，不能由 root 代写，
// 否则后续 runtime 无法按保留策略清理它。
func tryFinalizeSkillMigrationAsServiceUser(ctx context.Context, request Request, executable, home string) (bool, error) {
	return runSkillCommandAsServiceUser(
		ctx,
		request,
		executable,
		home,
		"skill migration finalize",
		"skill", "finalize-migration",
	)
}

func runSkillCommandAsServiceUser(
	ctx context.Context,
	request Request,
	executable, home, operation string,
	args ...string,
) (bool, error) {
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
		return true, fmt.Errorf("%s 缺少已安装 live binary", operation)
	}
	// 必须 re-exec 已经切换到 install root 且显式 chmod 0755 的 live binary。
	// 当前 Engine 可能运行在安装用户的 0700 临时目录里；降权后再 exec 那个
	// 临时 payload 会因 service user 无法穿过目录而 EACCES。
	// 环境只保留 Skill 生命周期命令所需的最小集合，避免把 root 侧 secrets
	// 泄漏进降权子进程。
	cmd := exec.CommandContext(ctx, executable, args...)
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
		return true, fmt.Errorf("%s (as %s): %w: %s", operation, username, err, strings.TrimSpace(string(output)))
	}
	return true, nil
}
