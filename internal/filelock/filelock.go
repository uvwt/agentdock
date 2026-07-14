package filelock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	ownerPrefix       = "owner-"
	pollDelay         = 25 * time.Millisecond
	staleAfter        = 10 * time.Minute
	heartbeatInterval = staleAfter / 3
	emptyLockGrace    = 250 * time.Millisecond
	removeRetryDelay  = 10 * time.Millisecond
	removeRetryCount  = 50
)

// Acquire uses an owner-tagged directory as a portable cross-process lock.
// A stale lock is removed only when its contents have the exact shape created
// by this package; unknown files are never deleted automatically.
func Acquire(ctx context.Context, path string) (func(), error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("file lock path is required")
	}
	owner, err := newOwner()
	if err != nil {
		return nil, fmt.Errorf("create file lock owner: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create file lock parent: %w", err)
	}

	ticker := time.NewTicker(pollDelay)
	defer ticker.Stop()
	for {
		err := os.Mkdir(path, 0o700)
		if err == nil {
			ownerPath := filepath.Join(path, ownerPrefix+owner)
			if err := os.WriteFile(ownerPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
				cleanupErr := cleanupInitialization(path, ownerPath)
				// 竞争者可能刚好清理了一个没有 owner 的残留目录；重新抢锁即可。
				if errors.Is(err, os.ErrNotExist) && cleanupErr == nil {
					continue
				}
				return nil, errors.Join(fmt.Errorf("write file lock owner: %w", err), cleanupErr)
			}
			stopHeartbeat := maintainHeartbeat(ownerPath)
			var releaseOnce sync.Once
			return func() {
				releaseOnce.Do(func() {
					stopHeartbeat()
					release(path, owner)
				})
			}, nil
		}
		if !retryableLockCreationError(err) {
			return nil, fmt.Errorf("acquire file lock %s: %w", path, err)
		}
		if removeSafeStale(path, time.Now()) {
			continue
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("acquire file lock %s: %w", path, ctx.Err())
		case <-ticker.C:
		}
	}
}

func maintainHeartbeat(ownerPath string) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				if err := os.Chtimes(ownerPath, now, now); err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						slog.Warn("refresh file lock heartbeat failed", "path", ownerPath, "error", err)
					}
					return
				}
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func newOwner() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func cleanupInitialization(lockPath, ownerPath string) error {
	var cleanupErrors []error
	if err := os.Remove(ownerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("remove incomplete lock owner: %w", err))
	}
	if !removeLockDirectory(lockPath) {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("remove incomplete lock directory %s", lockPath))
	}
	return errors.Join(cleanupErrors...)
}

func release(lockPath, owner string) {
	ownerPath := filepath.Join(lockPath, ownerPrefix+owner)
	if err := os.Remove(ownerPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("remove file lock owner failed", "path", ownerPath, "error", err)
		}
		return
	}
	if !removeLockDirectory(lockPath) {
		slog.Warn("release file lock failed", "path", lockPath)
	}
}

func removeSafeStale(lockPath string, now time.Time) bool {
	entries, err := os.ReadDir(lockPath)
	if err != nil {
		return errors.Is(err, os.ErrNotExist)
	}
	if len(entries) == 0 {
		info, statErr := os.Stat(lockPath)
		if statErr != nil {
			return errors.Is(statErr, os.ErrNotExist)
		}
		// 正常初始化只会短暂为空；超过宽限期仍为空说明释放或初始化中断。
		if now.Sub(info.ModTime()) <= emptyLockGrace {
			return false
		}
		return removeLockDirectory(lockPath)
	}
	if len(entries) != 1 || entries[0].IsDir() || !validOwnerName(entries[0].Name()) {
		return false
	}
	info, err := entries[0].Info()
	if err != nil {
		return errors.Is(err, os.ErrNotExist)
	}
	if now.Sub(info.ModTime()) <= staleAfter {
		return false
	}
	ownerPath := filepath.Join(lockPath, entries[0].Name())
	if ownerPIDAlive(ownerPath) {
		return false
	}
	if err := os.Remove(ownerPath); err != nil {
		return errors.Is(err, os.ErrNotExist)
	}
	return removeLockDirectory(lockPath)
}

func ownerPIDAlive(ownerPath string) bool {
	file, err := os.Open(ownerPath)
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65))
	if err != nil || len(data) > 64 {
		return true
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return true
	}
	return processAlive(pid)
}

func validOwnerName(name string) bool {
	raw := strings.TrimPrefix(name, ownerPrefix)
	if raw == name || len(raw) != 32 {
		return false
	}
	decoded, err := hex.DecodeString(raw)
	return err == nil && len(decoded) == 16
}

func removeLockDirectory(path string) bool {
	for attempt := 0; attempt < removeRetryCount; attempt++ {
		err := os.Remove(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return true
		}
		entries, readErr := os.ReadDir(path)
		if readErr == nil && len(entries) > 0 {
			return false
		}
		if errors.Is(readErr, os.ErrNotExist) {
			return true
		}
		if attempt+1 < removeRetryCount {
			time.Sleep(removeRetryDelay)
		}
	}
	return false
}
