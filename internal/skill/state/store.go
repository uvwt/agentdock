package state

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/fs/filelock"
	"github.com/uvwt/agentdock/internal/fs/securepath"
)

const (
	lockRetryInterval = 25 * time.Millisecond
	readerStaleAfter  = 25 * time.Hour
	readerOwnerPrefix = "reader-"
)

type Store struct {
	root            string
	lockRoot        string
	tempRoot        string
	transactionRoot string
}

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("managed Skill root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve managed Skill root: %w", err)
	}
	home := filepath.Dir(abs)
	store := &Store{
		root:            abs,
		lockRoot:        filepath.Join(home, "locks", "skills"),
		tempRoot:        filepath.Join(home, "tmp", "skills"),
		transactionRoot: filepath.Join(home, "state", "skills", "transactions"),
	}
	if err := store.EnsureLayout(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) EnsureLayout() error {
	for _, item := range []struct {
		name string
		path string
	}{
		{name: "managed Skills", path: s.root},
		{name: "Skill locks", path: s.lockRoot},
		{name: "Skill temporary files", path: s.tempRoot},
		{name: "Skill replacement transactions", path: s.transactionRoot},
	} {
		if err := os.MkdirAll(item.path, 0o700); err != nil {
			return fmt.Errorf("create %s directory: %w", item.name, err)
		}
		if err := securepath.EnsurePrivate(item.path); err != nil {
			return fmt.Errorf("secure %s directory: %w", item.name, err)
		}
	}
	return nil
}

func (s *Store) SkillPath(skill string) (string, error) {
	if err := validateIdentifier("skill", skill); err != nil {
		return "", err
	}
	return filepath.Join(s.root, skill), nil
}

func (s *Store) IsInstalled(skill string) (bool, error) {
	path, err := s.SkillPath(skill)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir() && info.Mode()&os.ModeSymlink == 0, nil
}

func (s *Store) ListSkills() ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if err := validateIdentifier("skill", entry.Name()); err != nil {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func (s *Store) Resolve(skill string) (string, error) {
	path, err := s.SkillPath(skill)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("skill %s is not installed", skill)
		}
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("skill %s is not a regular managed directory", skill)
	}
	return path, nil
}

func (s *Store) TempPath(prefix string) (string, error) {
	if err := validateIdentifier("temporary prefix", prefix); err != nil {
		return "", err
	}
	return os.MkdirTemp(s.tempRoot, prefix+"-")
}

func (s *Store) AcquireRead(ctx context.Context, skill string) (func(), error) {
	return s.acquireRead(ctx, strings.TrimSpace(skill))
}

func (s *Store) AcquireWrite(ctx context.Context, skill string) (func(), error) {
	return s.acquireWrite(ctx, strings.TrimSpace(skill))
}

func (s *Store) acquireRead(ctx context.Context, skill string) (func(), error) {
	if err := validateIdentifier("skill", skill); err != nil {
		return nil, err
	}
	readers, writer, err := s.componentLockPaths(skill)
	if err != nil {
		return nil, err
	}
	owner, err := newLockOwner()
	if err != nil {
		return nil, fmt.Errorf("create Skill reader owner: %w", err)
	}
	readerPath := filepath.Join(readers, readerOwnerPrefix+owner)
	ticker := time.NewTicker(lockRetryInterval)
	defer ticker.Stop()

	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("acquire Skill read lock: %w", err)
		}
		if _, err := os.Stat(writer); err == nil {
			if err := waitForLockRetry(ctx, ticker); err != nil {
				return nil, err
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect Skill writer lock: %w", err)
		}

		file, err := os.OpenFile(readerPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create Skill reader lock: %w", err)
		}
		if closeErr := file.Close(); closeErr != nil {
			_ = os.Remove(readerPath)
			return nil, fmt.Errorf("close Skill reader lock: %w", closeErr)
		}

		if _, err := os.Stat(writer); errors.Is(err, os.ErrNotExist) {
			if ctxErr := ctx.Err(); ctxErr != nil {
				_ = os.Remove(readerPath)
				return nil, fmt.Errorf("acquire Skill read lock: %w", ctxErr)
			}
			return func() {
				if err := os.Remove(readerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					slog.Warn("release Skill reader lock failed", "path", readerPath, "error", err)
				}
			}, nil
		} else if err != nil {
			_ = os.Remove(readerPath)
			return nil, fmt.Errorf("recheck Skill writer lock: %w", err)
		}

		_ = os.Remove(readerPath)
		if err := waitForLockRetry(ctx, ticker); err != nil {
			return nil, err
		}
	}
}

func (s *Store) acquireWrite(ctx context.Context, skill string) (func(), error) {
	if err := validateIdentifier("skill", skill); err != nil {
		return nil, err
	}
	readers, writer, err := s.componentLockPaths(skill)
	if err != nil {
		return nil, err
	}
	release, err := filelock.Acquire(ctx, writer)
	if err != nil {
		return nil, fmt.Errorf("acquire Skill writer lock: %w", err)
	}

	ticker := time.NewTicker(lockRetryInterval)
	defer ticker.Stop()
	for {
		empty, err := readersEmpty(readers)
		if err != nil {
			release()
			return nil, err
		}
		if empty {
			if ctxErr := ctx.Err(); ctxErr != nil {
				release()
				return nil, fmt.Errorf("acquire Skill write lock: %w", ctxErr)
			}
			return release, nil
		}
		cleanupStaleReaders(readers)
		if err := waitForLockRetry(ctx, ticker); err != nil {
			release()
			return nil, err
		}
	}
}

func (s *Store) componentLockPaths(skill string) (readers string, writer string, err error) {
	if err := validateIdentifier("skill", skill); err != nil {
		return "", "", err
	}
	component := filepath.Join(s.lockRoot, skill)
	readers = filepath.Join(component, "readers")
	if err := os.MkdirAll(readers, 0o700); err != nil {
		return "", "", fmt.Errorf("create Skill component lock directory: %w", err)
	}
	return readers, filepath.Join(component, "writer.lock"), nil
}

func readersEmpty(readers string) (bool, error) {
	entries, err := os.ReadDir(readers)
	if err != nil {
		return false, fmt.Errorf("read Skill reader locks: %w", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), readerOwnerPrefix) {
			return false, nil
		}
	}
	return true, nil
}

func cleanupStaleReaders(readers string) {
	entries, err := os.ReadDir(readers)
	if err != nil {
		return
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), readerOwnerPrefix) {
			continue
		}
		path := filepath.Join(readers, entry.Name())
		info, err := entry.Info()
		if err == nil && now.Sub(info.ModTime()) > readerStaleAfter {
			_ = os.Remove(path)
		}
	}
}

func waitForLockRetry(ctx context.Context, ticker *time.Ticker) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("acquire Skill lock: %w", ctx.Err())
	case <-ticker.C:
		return nil
	}
}

func newLockOwner() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func validateIdentifier(label, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if value == "." || value == ".." || filepath.IsAbs(value) ||
		strings.ContainsAny(value, `/\\`) || filepath.Base(value) != value {
		return fmt.Errorf("%s %q contains an invalid path segment", label, value)
	}
	return nil
}
