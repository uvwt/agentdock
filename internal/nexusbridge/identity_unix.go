//go:build !windows

package nexusbridge

import (
	"os"
	"path/filepath"
	"syscall"
)

type fileOwner struct {
	uid int
	gid int
}

// detectInheritedOwner 在 root 执行配对时向上查找已有数据目录的非 root 属主。
// atomicfile.Write 通过临时文件 rename 替换目标 inode；若不保留服务用户属主，
// sudo 执行配对会把 device.json 写成 root:root 0600，导致低权限服务重启后无权读取。
func detectInheritedOwner(agentDockHome string) (fileOwner, bool) {
	if os.Geteuid() != 0 {
		return fileOwner{}, false
	}
	return detectNonRootAncestorOwner(identityPath(agentDockHome), agentDockHome)
}

func detectNonRootAncestorOwner(targetPath, agentDockHome string) (fileOwner, bool) {
	cleanedHome := filepath.Clean(agentDockHome)
	candidates := []string{
		targetPath,
		filepath.Dir(targetPath),
		cleanedHome,
		filepath.Dir(cleanedHome),
	}
	for _, candidate := range candidates {
		if candidate == "" || candidate == "." || candidate == string(filepath.Separator) {
			continue
		}
		info, err := os.Lstat(candidate)
		if err != nil {
			continue
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fileOwner{}, false
		}
		if stat.Uid == 0 {
			continue
		}
		return fileOwner{uid: int(stat.Uid), gid: int(stat.Gid)}, true
	}
	return fileOwner{}, false
}

func applyInheritedOwner(agentDockHome, targetPath string, owner fileOwner) error {
	for _, candidate := range []string{
		filepath.Clean(agentDockHome),
		filepath.Dir(targetPath),
		targetPath,
	} {
		info, err := os.Lstat(candidate)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if ok && int(stat.Uid) == owner.uid && int(stat.Gid) == owner.gid {
			continue
		}
		if err := os.Lchown(candidate, owner.uid, owner.gid); err != nil {
			return err
		}
	}
	return nil
}
