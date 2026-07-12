package envstore

import (
	"fmt"
	"os"
	"path/filepath"
)

func (s *Store) ensureDirectories() error {
	for _, path := range []string{s.root, filepath.Join(s.root, string(ScopeMCP)), filepath.Join(s.root, string(ScopeSkill))} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create environment directory %q: %w", path, err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return fmt.Errorf("secure environment directory %q: %w", path, err)
		}
		if err := verifyMode(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func secureFile(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure environment file %q: %w", path, err)
	}
	return verifyMode(path, 0o600)
}

func verifyMode(path string, expected os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("verify permissions for %q: %w", path, err)
	}
	if actual := info.Mode().Perm(); actual != expected {
		return fmt.Errorf("permissions for %q are %04o, expected %04o", path, actual, expected)
	}
	return nil
}
