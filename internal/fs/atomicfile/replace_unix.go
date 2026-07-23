//go:build darwin || linux

package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

func replaceFile(source, target string) error {
	if err := os.Rename(source, target); err != nil {
		return err
	}
	// 文件本身已 fsync；再同步父目录，保证 rename 在断电后也可恢复。
	dir, err := os.Open(filepath.Dir(target))
	if err != nil {
		return fmt.Errorf("open atomic file parent directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync atomic file parent directory: %w", err)
	}
	return nil
}
