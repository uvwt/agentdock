package taskstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/fs/filelock"
)

type Store struct {
	root string
	mu   sync.Mutex
}

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("task state root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve task state root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create task state root: %w", err)
	}
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, fmt.Errorf("secure task state root: %w", err)
	}
	return &Store{root: abs}, nil
}

func (s *Store) acquireStoreLock() (func(), error) {
	s.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	releaseFileLock, err := filelock.Acquire(ctx, filepath.Join(s.root, ".store.lock"))
	cancel()
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("lock task state: %w", err)
	}
	return func() {
		releaseFileLock()
		s.mu.Unlock()
	}, nil
}

func (s *Store) Root() string { return s.root }
