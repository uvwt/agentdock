package envstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
	"github.com/uvwt/agentdock/internal/fs/filelock"
)

type ScopeKind string

const (
	ScopeSkill ScopeKind = "skill"
	ScopeMCP   ScopeKind = "mcp"

	maxEnvironmentFileBytes = 1 << 20
)

type Scope struct {
	Kind ScopeKind
	Name string
}

type Entry struct {
	Key        string `json:"key"`
	Configured bool   `json:"configured"`
}

type Store struct {
	root string
	mu   sync.Mutex
}

func New(agentDockHome string) (*Store, error) {
	if agentDockHome == "" {
		return nil, errors.New("agentdock home is required")
	}
	store := &Store{root: filepath.Join(agentDockHome, "env")}
	if err := store.ensureDirectories(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) acquireStoreLock() (func(), error) {
	s.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	releaseFileLock, err := filelock.Acquire(ctx, filepath.Join(s.root, ".store.lock"))
	cancel()
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("lock environment store: %w", err)
	}
	return func() {
		releaseFileLock()
		s.mu.Unlock()
	}, nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) Path(scope Scope) (string, error) {
	if err := validateScope(scope); err != nil {
		return "", err
	}
	return filepath.Join(s.root, string(scope.Kind), scope.Name+".env"), nil
}

func (s *Store) Set(scope Scope, key, value string) error {
	if err := validateScope(scope); err != nil {
		return err
	}
	if err := ValidateKey(key); err != nil {
		return err
	}

	release, err := s.acquireStoreLock()
	if err != nil {
		return err
	}
	defer release()

	values, err := s.loadLocked(scope)
	if err != nil {
		return err
	}
	values[key] = value
	return s.writeLocked(scope, values)
}

func (s *Store) Unset(scope Scope, key string) (bool, error) {
	if err := validateScope(scope); err != nil {
		return false, err
	}
	if err := ValidateKey(key); err != nil {
		return false, err
	}

	release, err := s.acquireStoreLock()
	if err != nil {
		return false, err
	}
	defer release()

	values, err := s.loadLocked(scope)
	if err != nil {
		return false, err
	}
	if _, exists := values[key]; !exists {
		return false, nil
	}
	delete(values, key)
	if err := s.writeLocked(scope, values); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) List(scope Scope) ([]Entry, error) {
	values, err := s.Load(scope)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]Entry, 0, len(keys))
	for _, key := range keys {
		items = append(items, Entry{Key: key, Configured: values[key] != ""})
	}
	return items, nil
}

func (s *Store) Load(scope Scope) (map[string]string, error) {
	if err := validateScope(scope); err != nil {
		return nil, err
	}

	release, err := s.acquireStoreLock()
	if err != nil {
		return nil, err
	}
	defer release()
	return s.loadLocked(scope)
}

func (s *Store) loadLocked(scope Scope) (map[string]string, error) {
	if err := s.ensureDirectories(); err != nil {
		return nil, err
	}
	path, err := s.Path(scope)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s environment: %w", scope.Kind, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxEnvironmentFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s environment: %w", scope.Kind, err)
	}
	if len(data) > maxEnvironmentFileBytes {
		return nil, fmt.Errorf("%s environment exceeds %d bytes", scope.Kind, maxEnvironmentFileBytes)
	}
	if err := secureFile(path); err != nil {
		return nil, err
	}
	values, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s environment %q: %w", scope.Kind, scope.Name, err)
	}
	return values, nil
}

func (s *Store) writeLocked(scope Scope, values map[string]string) error {
	path, err := s.Path(scope)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove empty %s environment: %w", scope.Kind, err)
		}
		return nil
	}
	for key := range values {
		if err := ValidateKey(key); err != nil {
			return err
		}
	}
	data := marshal(values)
	if len(data) > maxEnvironmentFileBytes {
		return fmt.Errorf("%s environment exceeds %d bytes", scope.Kind, maxEnvironmentFileBytes)
	}
	if err := atomicfile.Write(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s environment: %w", scope.Kind, err)
	}
	return secureFile(path)
}
