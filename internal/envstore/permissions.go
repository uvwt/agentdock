package envstore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/uvwt/agentdock/internal/fs/securepath"
)

func (s *Store) ensureDirectories() error {
	for _, path := range []string{s.root, filepath.Join(s.root, string(ScopeMCP)), filepath.Join(s.root, string(ScopeSkill))} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create environment directory %q: %w", path, err)
		}
		if err := securepath.EnsurePrivate(path); err != nil {
			return fmt.Errorf("secure environment directory %q: %w", path, err)
		}
	}
	return nil
}

func secureFile(path string) error {
	if err := securepath.EnsurePrivate(path); err != nil {
		return fmt.Errorf("secure environment file %q: %w", path, err)
	}
	return nil
}
