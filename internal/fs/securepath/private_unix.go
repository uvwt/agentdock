//go:build darwin || linux

package securepath

import (
	"fmt"
	"os"
)

// EnsurePrivate limits a file or directory to the current Unix user.
func EnsurePrivate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info.IsDir() {
		mode = 0o700
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("secure private path %s: %w", path, err)
	}
	return nil
}
