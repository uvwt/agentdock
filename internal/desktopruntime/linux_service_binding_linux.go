//go:build linux

package desktopruntime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/uvwt/agentdock/internal/envstore"
)

func DetectLinuxServiceBinding(runtimeRoot string) (LinuxServiceBinding, error) {
	if strings.TrimSpace(runtimeRoot) == "" {
		runtimeRoot = DefaultRuntimeRoot()
	}
	cleanedRoot := filepath.Clean(strings.TrimSpace(runtimeRoot))
	if cleanedRoot == "" || cleanedRoot == "." {
		return LinuxServiceBinding{}, nil
	}

	manifest, resolvedRoot, err := loadUnixRuntime(cleanedRoot)
	if err != nil {
		return LinuxServiceBinding{}, err
	}

	binding := LinuxServiceBinding{
		RuntimeRoot:    resolvedRoot,
		ServiceManager: linuxServiceManager(manifest),
		ServiceName:    strings.TrimSpace(manifest.ServiceName),
		ownerUID:       -1,
		ownerGID:       -1,
	}

	var configFound bool
	if _, statErr := os.Stat(filepath.Join(resolvedRoot, "desktop-runtime.json")); statErr == nil {
		configFound = true
	} else if !os.IsNotExist(statErr) {
		return LinuxServiceBinding{}, fmt.Errorf("检查桌面运行清单失败: %w", statErr)
	}

	envValues, envErr := envstore.ParseFile(manifest.EnvironmentFile)
	if envErr == nil {
		configFound = true
		binding.AgentDockHome = strings.TrimSpace(envValues["AGENTDOCK_HOME"])
		if info, statErr := os.Lstat(manifest.EnvironmentFile); statErr == nil {
			if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Uid != 0 {
				binding.ownerUID = int(stat.Uid)
				binding.ownerGID = int(stat.Gid)
			}
		}
	} else if !os.IsNotExist(envErr) {
		return LinuxServiceBinding{}, fmt.Errorf("读取服务环境变量文件: %w", envErr)
	}

	if !configFound {
		return LinuxServiceBinding{}, nil
	}

	if binding.ownerUID < 0 {
		if info, statErr := os.Lstat(resolvedRoot); statErr == nil {
			if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Uid != 0 {
				binding.ownerUID = int(stat.Uid)
				binding.ownerGID = int(stat.Gid)
			}
		}
	}

	binding.Configured = true
	return binding, nil
}

// SyncAgentDockHomeOwnership 在 root 为非 root 服务用户预先创建或更新 AGENTDOCK_HOME
// 下的子路径时，将路径属主收敛为服务用户，防止新目录以 root:root 落盘阻塞服务启动。
func (b LinuxServiceBinding) SyncAgentDockHomeOwnership(paths ...string) error {
	if os.Geteuid() != 0 || b.ownerUID <= 0 {
		return nil
	}
	for _, p := range paths {
		cleaned := filepath.Clean(strings.TrimSpace(p))
		if cleaned == "" || cleaned == "." || cleaned == string(filepath.Separator) {
			continue
		}
		info, err := os.Lstat(cleaned)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) == b.ownerUID && int(stat.Gid) == b.ownerGID {
			continue
		}
		if err := os.Lchown(cleaned, b.ownerUID, b.ownerGID); err != nil {
			return err
		}
	}
	return nil
}
