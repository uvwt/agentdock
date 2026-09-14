package installer

import (
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Linux 上 Engine 常由 wrapper 以 root 执行（run_root），而 install.sh 为了保护
// env 文件设置了 umask 077。Engine 创建的共享产物因此会低于服务运行所需的权限：
// live binary 必须可被 service user 执行，runtime root 与可变 env 必须允许
// service user 读写（读取配置、quick 地址回写）。Installer journal 不属于运行时，
// 不能为了方便把整棵 runtime root 递归改成 service user 所有。
// 这些权限不能依赖创建时的 umask，
// 必须在 activate 结束前显式收敛。

// applyLinuxRuntimeOwnership 收敛 Linux 共享产物的 mode/owner。
// 非 Linux 或非 root 时为 no-op（macOS/Windows 安装以当前用户身份运行）。
func applyLinuxRuntimeOwnership(request Request, staged stagedInstall) error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return nil
	}

	// 安装目录树：目录可穿越、live binary 可执行。
	for _, dir := range []string{
		request.InstallRoot,
		filepath.Join(request.InstallRoot, "bin"),
		filepath.Join(request.InstallRoot, "versions"),
	} {
		if err := chmodPath(dir, 0o755); err != nil {
			return err
		}
	}
	if versionsRoot := filepath.Join(request.InstallRoot, "versions"); dirExists(versionsRoot) {
		if err := chmodDirsUnder(versionsRoot, 0o755); err != nil {
			return err
		}
	}
	if staged.GenerationDir != "" {
		// generation 树会被 service user 读取（skill bundle manifest/packages），
		// 目录 0755、常规文件 0644、二进制 0755，全部与 umask 无关。
		if err := chmodGenerationTree(staged.GenerationDir); err != nil {
			return err
		}
	}
	if staged.LiveBinary != "" {
		if err := chmodPath(staged.LiveBinary, 0o755); err != nil {
			return err
		}
	}

	// runtime root 本身需要 service user 可写：Quick Tunnel 会用原子 rename
	// 更新 agentdock.env，并创建 quick-tunnel-url.txt。只调整目录和运行时文件；
	// install/transaction.json、result 与 rollback journal 继续保持 root ownership。
	serviceUser := strings.TrimSpace(request.ServiceUser)
	runtimeRoot := strings.TrimSpace(request.RuntimeRoot)
	if serviceUser == "" || runtimeRoot == "" {
		return nil
	}
	u, err := user.Lookup(serviceUser)
	if err != nil {
		return fmt.Errorf("解析 service user %s: %w", serviceUser, err)
	}
	uid, err := parseOwnershipID(u.Uid)
	if err != nil {
		return fmt.Errorf("service user %s uid 无效: %w", serviceUser, err)
	}
	gid, err := parseOwnershipID(u.Gid)
	if err != nil {
		return fmt.Errorf("service user %s gid 无效: %w", serviceUser, err)
	}
	if err := os.Chown(runtimeRoot, uid, gid); err != nil {
		return fmt.Errorf("chown runtime root %s: %w", runtimeRoot, err)
	}
	if err := os.Chmod(runtimeRoot, 0o700); err != nil {
		return fmt.Errorf("chmod runtime root %s: %w", runtimeRoot, err)
	}
	for _, name := range []string{
		"agentdock.env",
		"cloudflared.env",
		"desktop-runtime.json",
		"quick-tunnel-url.txt",
	} {
		if err := chownIfExists(filepath.Join(runtimeRoot, name), uid, gid); err != nil {
			return err
		}
	}
	return nil
}

func parseOwnershipID(value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		return 0, fmt.Errorf("id 不能为负数：%d", parsed)
	}
	return parsed, nil
}

func chmodPath(path string, mode os.FileMode) error {
	if err := os.Chmod(path, mode); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

// chmodGenerationTree 将 generation 树收敛为可被 service user 读取的状态：
// 目录 0755，常规文件 0644，agentdock 二进制 0755。
func chmodGenerationTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.Chmod(path, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if filepath.Base(path) == "agentdock" {
			return os.Chmod(path, 0o755)
		}
		return os.Chmod(path, 0o644)
	})
}

func chownIfExists(path string, uid, gid int) error {
	if err := os.Lchown(path, uid, gid); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	return nil
}

func chmodDirsUnder(root string, mode os.FileMode) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.Chmod(path, mode)
		}
		return nil
	})
}
