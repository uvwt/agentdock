package processlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Lock struct {
	file *os.File
}

func Acquire(ctx context.Context, path string) (*Lock, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("process lock path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create process lock directory: %w", err)
	}
	for {
		lock, acquired, err := tryAcquire(path)
		if err != nil {
			return nil, err
		}
		if acquired {
			return lock, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("acquire process lock %s: %w", path, ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func TryAcquire(path string) (*Lock, bool, error) {
	if strings.TrimSpace(path) == "" {
		return nil, false, errors.New("process lock path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, fmt.Errorf("create process lock directory: %w", err)
	}
	return tryAcquire(path)
}

func (lock *Lock) Release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	err := release(lock.file)
	lock.file = nil
	return err
}
