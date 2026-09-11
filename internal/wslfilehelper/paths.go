//go:build linux

package wslfilehelper

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var writeBlockedRoots = []string{"/proc", "/sys", "/dev", "/run"}

func checkedPath(value string, write bool) (string, error) {
	if value == "" || !strings.HasPrefix(value, "/") {
		return "", fail("INVALID_ARGUMENT", "WSL file paths must be absolute Linux paths", map[string]any{"path": value})
	}
	if strings.ContainsRune(value, 0) {
		return "", fail("INVALID_ARGUMENT", "path contains an invalid byte", map[string]any{"path": value})
	}
	path := filepath.Clean(value)
	if !write {
		return path, nil
	}
	for _, blocked := range writeBlockedRoots {
		if path == blocked || strings.HasPrefix(path, blocked+"/") {
			return "", fail("PROTECTED_WSL_PATH", "file_edit does not allow writes under protected WSL system paths", map[string]any{"path": path})
		}
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	current := string(filepath.Separator)
	for _, part := range parts[:max(0, len(parts)-1)] {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink path components", map[string]any{"path": current})
		}
	}
	return path, nil
}

func kindFromMode(mode fs.FileMode) string {
	switch {
	case mode.IsRegular():
		return "file"
	case mode.IsDir():
		return "directory"
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode&os.ModeNamedPipe != 0:
		return "fifo"
	case mode&os.ModeDevice != 0:
		return "device"
	default:
		return "other"
	}
}

func statIdentity(info fs.FileInfo) (mode, uid, gid int) {
	if raw, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(raw.Mode & 0o7777), int(raw.Uid), int(raw.Gid)
	}
	return int(info.Mode().Perm()), os.Geteuid(), os.Getegid()
}

func timestamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func hiddenPath(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") && part != "." && part != ".." {
			return true
		}
	}
	return false
}

func openNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink targets", map[string]any{"path": path})
		}
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
