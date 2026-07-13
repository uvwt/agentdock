package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Write replaces path with data only after the complete payload has been
// written and synced to a temporary file in the same directory.
func Write(path string, data []byte, mode os.FileMode) (returnErr error) {
	if path == "" {
		return fmt.Errorf("atomic file path is required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create atomic file directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".agentdock-atomic-*")
	if err != nil {
		return fmt.Errorf("create atomic temp file: %w", err)
	}
	tmpPath := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			if err := tmp.Close(); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("close atomic temp file during cleanup: %w", err))
			}
		}
		if err := os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			returnErr = errors.Join(returnErr, fmt.Errorf("remove atomic temp file: %w", err))
		}
	}()

	if err := tmp.Chmod(mode.Perm()); err != nil {
		return fmt.Errorf("set atomic temp file mode: %w", err)
	}
	// Windows 必须在写入敏感内容前收紧临时文件 DACL；Unix 在创建后已具备
	// 私有临时权限，secureWrittenFile 在对应平台为空操作。
	if err := secureWrittenFile(tmpPath, mode); err != nil {
		return fmt.Errorf("secure atomic temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write atomic temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync atomic temp file: %w", err)
	}
	closeErr := tmp.Close()
	closed = true
	if closeErr != nil {
		return fmt.Errorf("close atomic temp file: %w", closeErr)
	}
	if err := replaceFile(tmpPath, path); err != nil {
		return fmt.Errorf("replace atomic file: %w", err)
	}
	return nil
}
