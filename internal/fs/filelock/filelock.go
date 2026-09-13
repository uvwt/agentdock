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
	ownerPrefix          = "owner-"
	pollDelay            = 25 * time.Millisecond
	staleAfter           = 10 * time.Minute
	heartbeatInterval    = staleAfter / 3
	emptyLockGrace       = 2 * time.Second
	ownerWriteRetryDelay = 10 * time.Millisecond
	ownerWriteRetryCount = 100
	removeRetryDelay     = 10 * time.Millisecond
	removeRetryCount     = 50
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
acquireLoop:
	for {
		err := os.Mkdir(path, 0o700)
		if err == nil {
			// Windows may keep a just-removed directory in a transient delete-pending state.
			// Capture the directory identity so owner creation can retry without ever claiming
			// a different lock directory that another contender recreated at the same path.
			lockInfo, statErr := os.Stat(path)
			if statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					continue
				}
				return nil, fmt.Errorf("stat initialized file lock: %w", statErr)
			}
			ownerPath := filepath.Join(path, ownerPrefix+owner)
			ownerData := []byte(strconv.Itoa(os.Getpid()) + "\n")
			var ownerErr error
			for attempt := 0; attempt < ownerWriteRetryCount; attempt++ {
				ownerErr = os.WriteFile(ownerPath, ownerData, 0o600)
				currentInfo, identityErr := os.Stat(path)
				if identityErr != nil {
					if errors.Is(identityErr, os.ErrNotExist) {
						_ = os.Remove(ownerPath)
						continue acquireLoop
					}
					_ = os.Remove(ownerPath)
					return nil, fmt.Errorf("verify initialized file lock: %w", identityErr)
				}
				if !os.SameFile(lockInfo, currentInfo) {
					_ = os.Remove(ownerPath)
					continue acquireLoop
				}
				if ownerErr == nil {
					stopHeartbeat := maintainHeartbeat(ownerPath)
					var releaseOnce sync.Once
					return func() {
						releaseOnce.Do(func() {
							stopHeartbeat()
							release(path, owner)
						})
					}, nil
				}
				if attempt+1 < ownerWriteRetryCount {
					time.Sleep(ownerWriteRetryDelay)
				}
			}
			cleanupErr := cleanupInitialization(ownerPath)
			return nil, errors.Join(fmt.Errorf("write file lock owner: %w", ownerErr), cleanupErr)
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

func cleanupInitialization(ownerPath string) error {
	if err := os.Remove(ownerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove incomplete lock owner: %w", err)
	}
	// Do not remove the directory here. Another contender may have recreated the same
	// pathname between the failed owner write and cleanup. Empty directories are reclaimed
	// by removeSafeStale after emptyLockGrace without risking deletion of a new lock.
	return nil
}

func release(lockPath, owner string) {
	ownerPath := filepath.Join(lockPath, ownerPrefix+owner)
	// 空目录是可恢复状态，但正常释放也会短暂经过这个状态。先刷新目录时间，
	// 避免等待者在 owner 刚删除、目录尚未删除时把它误判为陈旧空锁。
	now := time.Now()
	_ = os.Chtimes(lockPath, now, now)
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
