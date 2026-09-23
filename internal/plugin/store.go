package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
	"github.com/uvwt/agentdock/internal/fs/filelock"
	"github.com/uvwt/agentdock/internal/fs/securepath"
)

const (
	maxStateBytes           = 2 << 20
	lockRetryInterval       = 25 * time.Millisecond
	readerStaleAfter        = 25 * time.Hour
	readerHeartbeatInterval = time.Hour
	readerOwnerPrefix       = "reader-"
)

type Store struct {
	home            string
	pluginRoot      string
	stateRoot       string
	transactionRoot string
	dataRoot        string
	tempRoot        string
	lockRoot        string

	mu    sync.Mutex
	locks map[string]*sync.RWMutex
}

func NewStore(agentDockHome string) (*Store, error) {
	home, err := filepath.Abs(strings.TrimSpace(agentDockHome))
	if err != nil || home == "." || strings.TrimSpace(agentDockHome) == "" {
		if err == nil {
			err = errors.New("AgentDock home is required")
		}
		return nil, fmt.Errorf("resolve Plugin home: %w", err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("create AgentDock home for Plugin storage: %w", err)
	}
	homeInfo, err := os.Lstat(home)
	if err != nil {
		return nil, fmt.Errorf("inspect AgentDock home for Plugin storage: %w", err)
	}
	if !homeInfo.IsDir() || homeInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("AgentDock home for Plugin storage must be a regular directory")
	}
	if err := securepath.EnsurePrivate(home); err != nil {
		return nil, err
	}
	store := &Store{
		home:            home,
		pluginRoot:      filepath.Join(home, "plugins"),
		stateRoot:       filepath.Join(home, "state", "plugins"),
		transactionRoot: filepath.Join(home, "state", "plugins", "transactions"),
		dataRoot:        filepath.Join(home, "data", "plugins"),
		tempRoot:        filepath.Join(home, "tmp", "plugins"),
		lockRoot:        filepath.Join(home, "locks", "plugins"),
		locks:           make(map[string]*sync.RWMutex),
	}
	if err := store.ensureLayout(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) ensureLayout() error {
	root, err := os.OpenRoot(s.home)
	if err != nil {
		return fmt.Errorf("open AgentDock home for Plugin layout: %w", err)
	}
	defer root.Close()
	for _, relative := range []string{
		"plugins",
		"state", filepath.Join("state", "plugins"), filepath.Join("state", "plugins", "transactions"),
		"data", filepath.Join("data", "plugins"),
		"tmp", filepath.Join("tmp", "plugins"),
		"locks", filepath.Join("locks", "plugins"),
	} {
		info, statErr := root.Lstat(relative)
		if errors.Is(statErr, os.ErrNotExist) {
			if mkdirErr := root.Mkdir(relative, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
				return fmt.Errorf("create Plugin layout %s: %w", relative, mkdirErr)
			}
			info, statErr = root.Lstat(relative)
		}
		if statErr != nil {
			return fmt.Errorf("inspect Plugin layout %s: %w", relative, statErr)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Plugin layout contains a symlink or non-directory component: %s", relative)
		}
		if err := securepath.EnsurePrivate(filepath.Join(s.home, relative)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) EnsurePackageParent(name string) (string, error) {
	name, err := pluginNamePathSegment(name)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(s.home)
	if err != nil {
		return "", err
	}
	defer root.Close()
	relative := filepath.Join("plugins", name)
	info, statErr := root.Lstat(relative)
	if errors.Is(statErr, os.ErrNotExist) {
		if mkdirErr := root.Mkdir(relative, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
			return "", fmt.Errorf("create Plugin package parent: %w", mkdirErr)
		}
		info, statErr = root.Lstat(relative)
	}
	if statErr != nil {
		return "", statErr
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("Plugin package parent contains a symlink or non-directory component")
	}
	path := filepath.Join(s.home, relative)
	if err := securepath.EnsurePrivate(path); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Store) Home() string { return s.home }

func (s *Store) PackagePath(name, version string) (string, error) {
	name, err := pluginNamePathSegment(name)
	if err != nil {
		return "", err
	}
	version, err = pluginVersionPathSegment(version)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.pluginRoot, name, version), nil
}

func (s *Store) StatePath(name string) (string, error) {
	name, err := pluginNamePathSegment(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.stateRoot, name+".json"), nil
}

func (s *Store) DataPath(name string) (string, error) {
	name, err := pluginNamePathSegment(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.dataRoot, name), nil
}

func (s *Store) TempPath(prefix string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || strings.ContainsAny(prefix, `/\\`) {
		return "", errors.New("invalid Plugin temporary prefix")
	}
	prefix = filepath.Base(prefix)
	return os.MkdirTemp(s.tempRoot, prefix+"-")
}

func (s *Store) Load(name string) (State, error) {
	name, err := pluginNamePathSegment(name)
	if err != nil {
		return State{}, err
	}
	root, err := os.OpenRoot(s.stateRoot)
	if err != nil {
		return State{}, err
	}
	defer root.Close()
	file, err := root.Open(name + ".json")
	if err != nil {
		return State{}, err
	}
	defer file.Close()
	return loadState(file)
}

func (s *Store) List() ([]State, error) {
	entries, err := os.ReadDir(s.stateRoot)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.stateRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	states := make([]State, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if ValidateName(name) != nil {
			continue
		}
		file, err := root.Open(entry.Name())
		if err != nil {
			return nil, err
		}
		state, loadErr := loadState(file)
		closeErr := file.Close()
		if loadErr != nil {
			return nil, loadErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Name < states[j].Name })
	return states, nil
}

func (s *Store) Save(state State) error {
	if err := validateState(state); err != nil {
		return err
	}
	path, err := s.StatePath(state.Name)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Plugin state destination is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxStateBytes {
		return fmt.Errorf("Plugin state exceeds %d bytes", maxStateBytes)
	}
	return atomicfile.Write(path, data, 0o600)
}

func (s *Store) Delete(name string) error {
	path, err := s.StatePath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) updateTransactionPath(name string) (string, error) {
	name, err := pluginNamePathSegment(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.transactionRoot, name+".json"), nil
}

func (s *Store) SaveUpdateTransaction(transaction UpdateTransaction) error {
	if err := validateUpdateTransaction(transaction); err != nil {
		return err
	}
	path, err := s.updateTransactionPath(transaction.Name)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(transaction, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxStateBytes {
		return fmt.Errorf("Plugin update transaction exceeds %d bytes", maxStateBytes)
	}
	return atomicfile.Write(path, data, 0o600)
}

func (s *Store) LoadUpdateTransaction(name string) (UpdateTransaction, error) {
	path, err := s.updateTransactionPath(name)
	if err != nil {
		return UpdateTransaction{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return UpdateTransaction{}, err
	}
	var transaction UpdateTransaction
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&transaction); err != nil {
		return UpdateTransaction{}, fmt.Errorf("decode Plugin update transaction: %w", err)
	}
	if err := validateUpdateTransaction(transaction); err != nil {
		return UpdateTransaction{}, err
	}
	return transaction, nil
}

func (s *Store) ListUpdateTransactions() ([]UpdateTransaction, error) {
	entries, err := os.ReadDir(s.transactionRoot)
	if err != nil {
		return nil, err
	}
	transactions := make([]UpdateTransaction, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		transaction, err := s.LoadUpdateTransaction(name)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, transaction)
	}
	return transactions, nil
}

func (s *Store) DeleteUpdateTransaction(name string) error {
	path, err := s.updateTransactionPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *Store) UpdateBackupPath(name string) (string, error) {
	parent, err := s.EnsurePackageParent(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, ".update-backup"), nil
}

func validateUpdateTransaction(transaction UpdateTransaction) error {
	if transaction.SchemaVersion != UpdateTransactionSchemaVersion {
		return fmt.Errorf("unsupported Plugin update transaction schema %d", transaction.SchemaVersion)
	}
	if err := ValidateName(transaction.Name); err != nil {
		return err
	}
	if transaction.Previous.Name != transaction.Name || transaction.Candidate.Name != transaction.Name {
		return errors.New("Plugin update transaction identity mismatch")
	}
	if err := validateState(transaction.Previous); err != nil {
		return fmt.Errorf("invalid previous Plugin state: %w", err)
	}
	if err := validateState(transaction.Candidate); err != nil {
		return fmt.Errorf("invalid candidate Plugin state: %w", err)
	}
	if transaction.Phase != "pending" && transaction.Phase != "committed" {
		return fmt.Errorf("invalid Plugin update transaction phase %q", transaction.Phase)
	}
	if transaction.CreatedAt.IsZero() {
		return errors.New("Plugin update transaction created_at is required")
	}
	return nil
}

func (s *Store) EnsureDataDir(name string) (string, error) {
	path, err := s.DataPath(name)
	if err != nil {
		return "", err
	}
	if err := ensurePrivateDirectoryPath(s.dataRoot, path); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Store) RemoveData(name string) error {
	path, err := s.DataPath(name)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Plugin data path is not a regular directory: %s", path)
	}
	return os.RemoveAll(path)
}

// AcquireBinding protects only the brief state->version binding window.
// Long-running Plugin work must use AcquireVersionRead after resolving state.
func (s *Store) AcquireBinding(ctx context.Context, name string) (func(), error) {
	name, err := pluginNamePathSegment(name)
	if err != nil {
		return nil, err
	}
	key, err := pluginLockKey(name)
	if err != nil {
		return nil, err
	}
	lock := s.localLock(name)
	lock.RLock()

	root, err := os.OpenRoot(s.lockRoot)
	if err != nil {
		lock.RUnlock()
		return nil, err
	}
	readers := key + ".binders"
	if err := root.MkdirAll(readers, 0o700); err != nil {
		root.Close()
		lock.RUnlock()
		return nil, fmt.Errorf("create Plugin binder directory: %w", err)
	}
	owner, err := newReaderOwner()
	if err != nil {
		root.Close()
		lock.RUnlock()
		return nil, err
	}
	readerPath := filepath.Join(readers, readerOwnerPrefix+owner)
	writer := key + ".lock"
	ticker := time.NewTicker(lockRetryInterval)
	defer ticker.Stop()

	for {
		if err := ctx.Err(); err != nil {
			root.Close()
			lock.RUnlock()
			return nil, fmt.Errorf("acquire Plugin binding: %w", err)
		}
		if _, err := root.Stat(writer); err == nil {
			select {
			case <-ctx.Done():
				root.Close()
				lock.RUnlock()
				return nil, fmt.Errorf("acquire Plugin binding: %w", ctx.Err())
			case <-ticker.C:
				continue
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			root.Close()
			lock.RUnlock()
			return nil, fmt.Errorf("inspect Plugin writer lock: %w", err)
		}

		file, err := root.OpenFile(readerPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			root.Close()
			lock.RUnlock()
			return nil, fmt.Errorf("create Plugin binder: %w", err)
		}
		if err := file.Close(); err != nil {
			_ = root.Remove(readerPath)
			root.Close()
			lock.RUnlock()
			return nil, fmt.Errorf("close Plugin binder: %w", err)
		}
		if _, err := root.Stat(writer); errors.Is(err, os.ErrNotExist) {
			stopHeartbeat := maintainPluginReaderHeartbeat(root, readerPath)
			var once sync.Once
			return func() {
				once.Do(func() {
					stopHeartbeat()
					if err := root.Remove(readerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
						slog.Warn("release Plugin binder failed", "plugin", name, "error", err)
					}
					_ = root.Close()
					lock.RUnlock()
				})
			}, nil
		} else if err != nil {
			_ = root.Remove(readerPath)
			root.Close()
			lock.RUnlock()
			return nil, fmt.Errorf("recheck Plugin writer lock: %w", err)
		}
		_ = root.Remove(readerPath)
		select {
		case <-ctx.Done():
			root.Close()
			lock.RUnlock()
			return nil, fmt.Errorf("acquire Plugin binding: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (s *Store) AcquireWrite(ctx context.Context, name string) (func(), error) {
	name, err := pluginNamePathSegment(name)
	if err != nil {
		return nil, err
	}
	key, err := pluginLockKey(name)
	if err != nil {
		return nil, err
	}
	lock := s.localLock(name)
	lock.Lock()
	writer := filepath.Join(s.lockRoot, key+".lock")
	releaseFile, err := filelock.Acquire(ctx, writer)
	if err != nil {
		lock.Unlock()
		return nil, err
	}

	root, err := os.OpenRoot(s.lockRoot)
	if err != nil {
		releaseFile()
		lock.Unlock()
		return nil, err
	}
	defer root.Close()
	readers := key + ".binders"
	if err := root.MkdirAll(readers, 0o700); err != nil {
		releaseFile()
		lock.Unlock()
		return nil, fmt.Errorf("create Plugin binder directory: %w", err)
	}

	ticker := time.NewTicker(lockRetryInterval)
	defer ticker.Stop()
	for {
		cleanupStalePluginReaders(root, readers)
		empty, err := pluginReadersEmpty(root, readers)
		if err != nil {
			releaseFile()
			lock.Unlock()
			return nil, err
		}
		if empty {
			var once sync.Once
			return func() {
				once.Do(func() {
					releaseFile()
					lock.Unlock()
				})
			}, nil
		}
		select {
		case <-ctx.Done():
			releaseFile()
			lock.Unlock()
			return nil, fmt.Errorf("acquire Plugin write lock: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (s *Store) AcquireVersionRead(name, version string) (func(), error) {
	key, err := pluginVersionLockKey(name, version)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.lockRoot)
	if err != nil {
		return nil, err
	}
	readers := filepath.Join("versions", key, "readers")
	if err := root.MkdirAll(readers, 0o700); err != nil {
		root.Close()
		return nil, fmt.Errorf("create Plugin version reader directory: %w", err)
	}
	owner, err := newReaderOwner()
	if err != nil {
		root.Close()
		return nil, err
	}
	readerPath := filepath.Join(readers, readerOwnerPrefix+owner)
	file, err := root.OpenFile(readerPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		root.Close()
		return nil, fmt.Errorf("create Plugin version reader lock: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = root.Remove(readerPath)
		root.Close()
		return nil, fmt.Errorf("close Plugin version reader lock: %w", err)
	}
	stopHeartbeat := maintainPluginReaderHeartbeat(root, readerPath)
	var once sync.Once
	return func() {
		once.Do(func() {
			stopHeartbeat()
			if err := root.Remove(readerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.Warn("release Plugin version reader lock failed", "error", err)
			}
			_ = root.Close()
		})
	}, nil
}

func (s *Store) WaitVersionIdle(ctx context.Context, name, version string) error {
	key, err := pluginVersionLockKey(name, version)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(s.lockRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	readers := filepath.Join("versions", key, "readers")
	if err := root.MkdirAll(readers, 0o700); err != nil {
		return err
	}
	ticker := time.NewTicker(lockRetryInterval)
	defer ticker.Stop()
	for {
		cleanupStalePluginReaders(root, readers)
		empty, err := pluginReadersEmpty(root, readers)
		if err != nil {
			return err
		}
		if empty {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for Plugin readers: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (s *Store) RemoveVersionIfIdle(name, version string) (bool, error) {
	key, err := pluginVersionLockKey(name, version)
	if err != nil {
		return false, err
	}
	root, err := os.OpenRoot(s.lockRoot)
	if err != nil {
		return false, err
	}
	readers := filepath.Join("versions", key, "readers")
	if err := root.MkdirAll(readers, 0o700); err != nil {
		root.Close()
		return false, err
	}
	cleanupStalePluginReaders(root, readers)
	empty, err := pluginReadersEmpty(root, readers)
	closeErr := root.Close()
	if err != nil {
		return false, err
	}
	if closeErr != nil {
		return false, closeErr
	}
	if !empty {
		return false, nil
	}

	name, err = pluginNamePathSegment(name)
	if err != nil {
		return false, err
	}
	version, err = pluginVersionPathSegment(version)
	if err != nil {
		return false, err
	}
	packageRoot, err := os.OpenRoot(s.pluginRoot)
	if err != nil {
		return false, err
	}
	defer packageRoot.Close()
	relative := filepath.Join(name, version)
	info, err := packageRoot.Lstat(relative)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("Plugin version path is not a regular directory")
	}
	if err := packageRoot.RemoveAll(relative); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) localLock(name string) *sync.RWMutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock := s.locks[name]
	if lock == nil {
		lock = &sync.RWMutex{}
		s.locks[name] = lock
	}
	return lock
}

func pluginReadersEmpty(root *os.Root, readers string) (bool, error) {
	dir, err := root.Open(readers)
	if err != nil {
		return false, fmt.Errorf("open Plugin reader directory: %w", err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return false, fmt.Errorf("read Plugin reader locks: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), readerOwnerPrefix) {
			return false, nil
		}
	}
	return true, nil
}

func maintainPluginReaderHeartbeat(root *os.Root, path string) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(readerHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				if err := root.Chtimes(path, now, now); err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						slog.Warn("refresh Plugin reader heartbeat failed", "error", err)
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

func cleanupStalePluginReaders(root *os.Root, readers string) {
	dir, err := root.Open(readers)
	if err != nil {
		return
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), readerOwnerPrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) <= readerStaleAfter {
			continue
		}
		_ = root.Remove(filepath.Join(readers, entry.Name()))
	}
}

func pluginLockKey(name string) (string, error) {
	name, err := pluginNamePathSegment(name)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte("plugin\x00" + name))
	return hex.EncodeToString(sum[:]), nil
}

func pluginVersionLockKey(name, version string) (string, error) {
	name, err := pluginNamePathSegment(name)
	if err != nil {
		return "", err
	}
	version, err = pluginVersionPathSegment(version)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte("plugin-version\x00" + name + "\x00" + version))
	return hex.EncodeToString(sum[:]), nil
}

func newReaderOwner() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("create Plugin reader owner: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// Plugin names and versions are logical identifiers, never filesystem paths.
// filepath.Base is intentionally part of the normalization boundary so static
// path-taint analysis can see one safe path segment before filesystem access.
func pluginNamePathSegment(value string) (string, error) {
	value = strings.TrimSpace(value)
	if err := ValidateName(value); err != nil {
		return "", err
	}
	segment := filepath.Base(value)
	if segment != value || segment == "." || segment == string(filepath.Separator) {
		return "", errors.New("Plugin name is not a filesystem segment")
	}
	return segment, nil
}

func pluginVersionPathSegment(value string) (string, error) {
	value = strings.TrimSpace(value)
	if err := ValidateVersion(value); err != nil {
		return "", err
	}
	segment := filepath.Base(value)
	if segment != value || segment == "." || segment == string(filepath.Separator) {
		return "", errors.New("Plugin version is not a filesystem segment")
	}
	return segment, nil
}

func loadState(reader io.Reader) (State, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxStateBytes+1))
	if err != nil {
		return State{}, err
	}
	if len(data) > maxStateBytes {
		return State{}, fmt.Errorf("Plugin state exceeds %d bytes", maxStateBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state State
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("decode Plugin state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return State{}, errors.New("Plugin state contains trailing JSON")
		}
		return State{}, err
	}
	if err := validateState(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func validateState(state State) error {
	if state.SchemaVersion != StateSchemaVersion {
		return fmt.Errorf("unsupported Plugin state schema %d", state.SchemaVersion)
	}
	if err := ValidateName(state.Name); err != nil {
		return err
	}
	if err := ValidateVersion(state.Version); err != nil {
		return err
	}
	if !strings.HasPrefix(state.PackageDigest, "sha256:") {
		return errors.New("Plugin package digest is required")
	}
	if state.Source.Type == "" {
		return errors.New("Plugin source type is required")
	}
	if state.Source.Type == "git" {
		if err := validateStoredGitSourceCredentials(state.Source.Ref); err != nil {
			return err
		}
	}
	if state.Source.ResolvedType == "git" {
		if err := validateStoredGitSourceCredentials(state.Source.ResolvedRef); err != nil {
			return err
		}
	}
	if state.InstalledAt.IsZero() {
		return errors.New("Plugin installed_at is required")
	}
	return nil
}

func ensurePrivateDirectoryPath(root, target string) error {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("Plugin data path escapes data root")
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return err
			}
			if err := securepath.EnsurePrivate(current); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Plugin data path component is not a regular directory: %s", current)
		}
		if err := securepath.EnsurePrivate(current); err != nil {
			return err
		}
	}
	return nil
}
