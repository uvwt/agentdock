package skillstate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/atomicfile"
	"github.com/uvwt/agentdock/internal/securepath"
)

type Channel string

const (
	ChannelDevelopment Channel = "development"
	ChannelCanary      Channel = "canary"
	ChannelStable      Channel = "stable"
	ChannelPinned      Channel = "pinned"
)

const (
	lockOwnerPrefix             = "owner-"
	lockRetryInterval           = 25 * time.Millisecond
	transientLockErrorRetryTime = 500 * time.Millisecond
)

var validChannels = map[Channel]struct{}{
	ChannelDevelopment: {},
	ChannelCanary:      {},
	ChannelStable:      {},
	ChannelPinned:      {},
}

type Selection struct {
	ActiveVersion string             `json:"active_version,omitempty"`
	Channels      map[Channel]string `json:"channels,omitempty"`
	History       []string           `json:"history,omitempty"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

type Store struct{ root string }

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("skill state root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve skill state root: %w", err)
	}
	s := &Store{root: abs}
	if err := s.EnsureLayout(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) EnsureLayout() error {
	for _, name := range []string{"installed", "cache", "state", "locks", "tmp"} {
		path := filepath.Join(s.root, name)
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create skill state directory %s: %w", name, err)
		}
		if err := securepath.EnsurePrivate(path); err != nil {
			return fmt.Errorf("secure skill state directory %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) InstalledPath(skill, version string) (string, error) {
	if err := validateIdentifier("skill", skill); err != nil {
		return "", err
	}
	if err := validateIdentifier("version", version); err != nil {
		return "", err
	}
	return filepath.Join(s.root, "installed", skill, version), nil
}

func (s *Store) CachePath(name string) (string, error) {
	if err := validateIdentifier("cache name", name); err != nil {
		return "", err
	}
	return filepath.Join(s.root, "cache", name), nil
}

func (s *Store) TempPath(prefix string) (string, error) {
	if err := validateIdentifier("temporary prefix", prefix); err != nil {
		return "", err
	}
	return os.MkdirTemp(filepath.Join(s.root, "tmp"), prefix+"-")
}

func (s *Store) IsInstalled(skill, version string) (bool, error) {
	p, err := s.InstalledPath(skill, version)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(p)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func (s *Store) ListVersions(skill string) ([]string, error) {
	if err := validateIdentifier("skill", skill); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(s.root, "installed", skill))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			versions = append(versions, entry.Name())
		}
	}
	sort.Strings(versions)
	return versions, nil
}

func (s *Store) ListSkills() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "installed"))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	skills := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := validateIdentifier("skill", entry.Name()); err != nil {
			continue
		}
		skills = append(skills, entry.Name())
	}
	sort.Strings(skills)
	return skills, nil
}

func (s *Store) ActiveVersion(skill string) (string, error) {
	if err := validateIdentifier("skill", skill); err != nil {
		return "", err
	}
	state, err := s.load(skill)
	if err != nil {
		return "", err
	}
	return state.ActiveVersion, nil
}

func (s *Store) Snapshot(skill string) (Selection, error) {
	if err := validateIdentifier("skill", skill); err != nil {
		return Selection{}, err
	}
	return s.load(skill)
}

func (s *Store) Resolve(skill, version string, channel Channel) (string, error) {
	if version != "" {
		installed, err := s.IsInstalled(skill, version)
		if err != nil {
			return "", err
		}
		if !installed {
			return "", fmt.Errorf("skill %s version %s is not installed", skill, version)
		}
		return s.InstalledPath(skill, version)
	}
	state, err := s.Snapshot(skill)
	if err != nil {
		return "", err
	}
	selected := ""
	if channel != "" {
		if _, ok := validChannels[channel]; !ok {
			return "", fmt.Errorf("invalid skill channel %q", channel)
		}
		selected = state.Channels[channel]
	}
	if selected == "" {
		selected = state.Channels[ChannelPinned]
	}
	if selected == "" {
		selected = state.ActiveVersion
	}
	if selected == "" {
		return "", fmt.Errorf("skill %s has no active version", skill)
	}
	return s.InstalledPath(skill, selected)
}

func (s *Store) Activate(ctx context.Context, skill, version string, channel Channel) error {
	if channel == "" {
		channel = ChannelStable
	}
	if _, ok := validChannels[channel]; !ok {
		return fmt.Errorf("invalid skill channel %q", channel)
	}
	release, err := s.acquire(ctx, skill)
	if err != nil {
		return err
	}
	defer release()

	// 安装存在性检查必须位于同一个 Skill 锁内；否则删除操作可能在检查后、
	// 激活前移除目标版本，最终留下指向不存在目录的 active 状态。
	installed, err := s.IsInstalled(skill, version)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("skill %s version %s is not installed", skill, version)
	}
	state, err := s.load(skill)
	if err != nil {
		return err
	}
	if state.Channels == nil {
		state.Channels = make(map[Channel]string)
	}
	if state.ActiveVersion != "" && state.ActiveVersion != version {
		state.History = prependUnique(state.History, state.ActiveVersion, 20)
	}
	state.ActiveVersion = version
	state.Channels[channel] = version
	state.UpdatedAt = time.Now().UTC()

	if err := s.save(skill, state); err != nil {
		return err
	}
	return nil
}

func (s *Store) PreviousVersion(skill string) (string, error) {
	state, err := s.Snapshot(skill)
	if err != nil {
		return "", err
	}
	for _, version := range state.History {
		installed, checkErr := s.IsInstalled(skill, version)
		if checkErr != nil {
			return "", checkErr
		}
		if installed {
			return version, nil
		}
	}
	return "", fmt.Errorf("skill %s has no rollback version", skill)
}

func (s *Store) RemoveVersion(ctx context.Context, skill, version string) error {
	if _, err := s.InstalledPath(skill, version); err != nil {
		return err
	}
	release, err := s.acquire(ctx, skill)
	if err != nil {
		return err
	}
	defer release()

	active, err := s.ActiveVersion(skill)
	if err != nil {
		return err
	}
	if active == version {
		return fmt.Errorf("cannot remove active skill version %s", version)
	}
	p, err := s.InstalledPath(skill, version)
	if err != nil {
		return err
	}
	return os.RemoveAll(p)
}

func (s *Store) load(skill string) (Selection, error) {
	path := filepath.Join(s.root, "state", skill+".json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Selection{Channels: make(map[Channel]string)}, nil
	}
	if err != nil {
		return Selection{}, fmt.Errorf("read skill state: %w", err)
	}
	var state Selection
	if err := json.Unmarshal(data, &state); err != nil {
		return Selection{}, fmt.Errorf("decode skill state: %w", err)
	}
	if state.Channels == nil {
		state.Channels = make(map[Channel]string)
	}
	return state, nil
}

func (s *Store) save(skill string, state Selection) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode skill state: %w", err)
	}
	path := filepath.Join(s.root, "state", skill+".json")
	if err := atomicfile.Write(path, data, 0o600); err != nil {
		return fmt.Errorf("replace skill state: %w", err)
	}
	return nil
}

func (s *Store) acquire(ctx context.Context, skill string) (func(), error) {
	if err := validateIdentifier("skill", skill); err != nil {
		return nil, err
	}
	owner, err := newLockOwner()
	if err != nil {
		return nil, fmt.Errorf("create skill lock owner: %w", err)
	}
	lockPath := filepath.Join(s.root, "locks", skill+".lock")
	ticker := time.NewTicker(lockRetryInterval)
	defer ticker.Stop()
	var transientErrorSince time.Time
	for {
		err := os.Mkdir(lockPath, 0o700)
		if err == nil {
			ownerPath := filepath.Join(lockPath, lockOwnerPrefix+owner)
			if err := os.WriteFile(ownerPath, nil, 0o600); err != nil {
				cleanupErr := cleanupOwnedLockInitialization(lockPath, ownerPath)
				return nil, errors.Join(fmt.Errorf("write skill lock owner: %w", err), cleanupErr)
			}
			return func() { releaseOwnedLock(lockPath, owner) }, nil
		}
		if os.IsExist(err) {
			transientErrorSince = time.Time{}
		} else if isTransientLockContention(err) {
			// Windows 删除目录时可能短暂处于 delete-pending 状态，此时同路径 Mkdir
			// 返回 Access Denied。只在有限窗口内重试，避免把真实权限错误无限掩盖。
			if transientErrorSince.IsZero() {
				transientErrorSince = time.Now()
			} else if time.Since(transientErrorSince) >= transientLockErrorRetryTime {
				return nil, fmt.Errorf("acquire skill lock: %w", err)
			}
		} else {
			return nil, fmt.Errorf("acquire skill lock: %w", err)
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 10*time.Minute {
			if removeStaleOwnedLock(lockPath) {
				continue
			}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("acquire skill lock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func newLockOwner() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func cleanupOwnedLockInitialization(lockPath, ownerPath string) error {
	var cleanupErrs []error
	if err := os.Remove(ownerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("remove incomplete skill lock owner: %w", err))
	}
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("remove incomplete skill lock directory: %w", err))
	}
	return errors.Join(cleanupErrs...)
}

func releaseOwnedLock(lockPath, owner string) {
	ownerPath := filepath.Join(lockPath, lockOwnerPrefix+owner)
	if err := os.Remove(ownerPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("remove skill lock owner failed", "path", ownerPath, "error", err)
		}
		return
	}
	if err := os.Remove(lockPath); err != nil &&
		!errors.Is(err, os.ErrNotExist) &&
		!isDirectoryBusy(err) {
		slog.Warn("release skill lock failed", "path", lockPath, "error", err)
	}
}

func removeStaleOwnedLock(lockPath string) bool {
	entries, err := os.ReadDir(lockPath)
	if err != nil {
		return errors.Is(err, os.ErrNotExist)
	}
	if len(entries) == 0 {
		err := os.Remove(lockPath)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return true
		}
		if !isDirectoryBusy(err) {
			slog.Warn("remove empty stale skill lock failed", "path", lockPath, "error", err)
		}
		return false
	}
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), lockOwnerPrefix) {
		return false
	}
	ownerPath := filepath.Join(lockPath, entries[0].Name())
	if err := os.Remove(ownerPath); err != nil {
		return errors.Is(err, os.ErrNotExist)
	}
	if err := os.Remove(lockPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		if !isDirectoryBusy(err) {
			slog.Warn("remove stale skill lock failed", "path", lockPath, "error", err)
		}
		return false
	}
	return true
}

func validateIdentifier(label, value string) error {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\\`) {
		return fmt.Errorf("invalid %s %q", label, value)
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+-", r) {
			continue
		}
		return fmt.Errorf("invalid %s %q", label, value)
	}
	return nil
}

func prependUnique(values []string, value string, max int) []string {
	out := []string{value}
	for _, existing := range values {
		if existing != value {
			out = append(out, existing)
		}
		if len(out) == max {
			break
		}
	}
	return out
}
