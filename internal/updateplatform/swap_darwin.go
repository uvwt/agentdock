//go:build darwin

package updateplatform

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// swapPathsAtomic exchanges two existing paths in one Darwin rename transaction.
// The update engine only uses it for sibling App Bundle directories on the same volume,
// which keeps AgentDock.app continuously present during activation and rollback.
func swapPathsAtomic(first, second string) error {
	firstAbs, err := filepath.Abs(first)
	if err != nil {
		return fmt.Errorf("resolve first swap path: %w", err)
	}
	secondAbs, err := filepath.Abs(second)
	if err != nil {
		return fmt.Errorf("resolve second swap path: %w", err)
	}
	if filepath.Dir(firstAbs) != filepath.Dir(secondAbs) {
		return fmt.Errorf("atomic swap requires sibling paths: %s, %s", firstAbs, secondAbs)
	}
	if err := unix.RenameatxNp(unix.AT_FDCWD, firstAbs, unix.AT_FDCWD, secondAbs, unix.RENAME_SWAP); err != nil {
		return fmt.Errorf("atomic swap %s <-> %s: %w", firstAbs, secondAbs, err)
	}
	parent, err := os.Open(filepath.Dir(firstAbs))
	if err != nil {
		return fmt.Errorf("open atomic swap parent: %w", err)
	}
	defer parent.Close()
	if err := parent.Sync(); err != nil {
		return fmt.Errorf("sync atomic swap parent: %w", err)
	}
	return nil
}
