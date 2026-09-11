//go:build linux

package wslfilehelper

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

type transactionItem struct {
	Path           string `json:"path"`
	ExpectedExists bool   `json:"expected_exists"`
	NewExists      bool   `json:"new_exists"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	ExpectedMode   *int   `json:"expected_mode,omitempty"`
	ExpectedUID    *int   `json:"expected_uid,omitempty"`
	ExpectedGID    *int   `json:"expected_gid,omitempty"`
	NewSHA256      string `json:"new_sha256,omitempty"`
	NewMode        *int   `json:"new_mode,omitempty"`
	NewUID         *int   `json:"new_uid,omitempty"`
	NewGID         *int   `json:"new_gid,omitempty"`
	TempPath       string `json:"temp_path,omitempty"`
	BackupPath     string `json:"backup_path,omitempty"`

	Content string `json:"-"`
}

type transactionJournal struct {
	Version       int               `json:"version"`
	TransactionID string            `json:"transaction_id"`
	Workdir       string            `json:"workdir"`
	Phase         string            `json:"phase"`
	CreatedDirs   []string          `json:"created_dirs"`
	Items         []transactionItem `json:"items"`
}

type fileSnapshot struct {
	SHA256 string
	Mode   int
	UID    int
	GID    int
}

// transactionOps 只封装事务真正存在故障边界的两个原语。
// 它保持包内私有，既能做 fault test，又不会为了测试污染生产 API。
type transactionOps struct {
	renameNoReplace  func(source, destination string) error
	fsyncDirStrict   func(path string) error
	cleanupCommitted func(journal transactionJournal, ops transactionOps) []string
}

func defaultTransactionOps() transactionOps {
	return transactionOps{
		renameNoReplace:  renameNoReplace,
		fsyncDirStrict:   fsyncDirectoryStrict,
		cleanupCommitted: cleanupCommittedTransaction,
	}
}

func transactionStateDir(workdir string) (string, error) {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	sum := sha256.Sum256([]byte(workdir))
	root := filepath.Join(stateHome, "agentdock", "wsl-patch-transactions", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(root, 0o700)
	return root, nil
}

func acquireTransactionLock(stateDir string) (*os.File, error) {
	lockPath := filepath.Join(stateDir, "lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, fail("PATCH_BUSY", "another WSL patch transaction is active for this workdir", nil)
		}
		return nil, err
	}
	return file, nil
}

func randomTransactionID() (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func writeJSONAtomic(path string, value any, ops transactionOps) error {
	parent := filepath.Dir(path)
	id, err := randomTransactionID()
	if err != nil {
		return err
	}
	temporary := path + ".tmp-" + id
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	replaced := false
	defer func() {
		_ = file.Close()
		if !replaced {
			_ = os.Remove(temporary)
		}
	}()
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	replaced = true
	// journal 的目录 fsync 是 crash recovery 契约的一部分，不能 best-effort。
	return ops.fsyncDirStrict(parent)
}

func pathSnapshot(path string) (*fileSnapshot, error) {
	path, err := checkedPath(path, true)
	if err != nil {
		return nil, err
	}
	file, err := openNoFollow(path)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fail("NOT_REGULAR_FILE", "file_edit patch only supports regular files", map[string]any{"path": path, "type": kindFromMode(info.Mode())})
	}
	if info.Size() > maxTextFileBytes {
		return nil, fail("FILE_TOO_LARGE", "patch target exceeds the text file input limit", map[string]any{"path": path, "size_bytes": info.Size()})
	}
	data, err := io.ReadAll(io.LimitReader(file, maxTextFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTextFileBytes {
		return nil, fail("FILE_TOO_LARGE", "patch target exceeds the text file input limit", map[string]any{"path": path})
	}
	mode, uid, gid := statIdentity(info)
	sum := sha256.Sum256(data)
	return &fileSnapshot{SHA256: hex.EncodeToString(sum[:]), Mode: mode, UID: uid, GID: gid}, nil
}

func snapshotMatches(snapshot *fileSnapshot, sha256Value string, mode, uid, gid *int) bool {
	if snapshot == nil || snapshot.SHA256 != sha256Value {
		return false
	}
	if mode != nil && snapshot.Mode != *mode {
		return false
	}
	if uid != nil && snapshot.UID != *uid {
		return false
	}
	if gid != nil && snapshot.GID != *gid {
		return false
	}
	return true
}

func missingParentDirectories(path string) ([]string, error) {
	missing := make([]string, 0)
	cursor := filepath.Dir(path)
	for {
		info, err := os.Lstat(cursor)
		if errors.Is(err, os.ErrNotExist) {
			missing = append(missing, cursor)
			parent := filepath.Dir(cursor)
			if parent == cursor {
				return nil, fail("PATH_NOT_FOUND", "patch parent directory does not exist", map[string]any{"path": path})
			}
			cursor = parent
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink path components", map[string]any{"path": cursor})
		}
		if !info.IsDir() {
			return nil, fail("NOT_A_DIRECTORY", "patch parent is not a directory", map[string]any{"path": cursor})
		}
		break
	}
	for left, right := 0, len(missing)-1; left < right; left, right = left+1, right-1 {
		missing[left], missing[right] = missing[right], missing[left]
	}
	return missing, nil
}

func normalizeTransactionChanges(req *Request, transactionID string) ([]transactionItem, []string, error) {
	if len(req.Changes) == 0 {
		return nil, nil, fail("INVALID_ARGUMENT", "patch transaction requires at least one staged change", nil)
	}
	items := make([]transactionItem, 0, len(req.Changes))
	seen := map[string]struct{}{}
	createdSeen := map[string]struct{}{}
	createdDirs := make([]string, 0)
	for index, raw := range req.Changes {
		path, err := checkedPath(raw.Path, true)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := seen[path]; exists {
			return nil, nil, fail("INVALID_ARGUMENT", "patch transaction contains a duplicate path", map[string]any{"path": path})
		}
		seen[path] = struct{}{}
		if raw.ExpectedExists == nil || raw.NewExists == nil {
			return nil, nil, fail("INVALID_ARGUMENT", "patch transaction existence fields must be booleans", map[string]any{"path": path})
		}
		item := transactionItem{
			Path: path, ExpectedExists: *raw.ExpectedExists, NewExists: *raw.NewExists,
			ExpectedSHA256: raw.ExpectedSHA256, ExpectedMode: raw.ExpectedMode, ExpectedUID: raw.ExpectedUID, ExpectedGID: raw.ExpectedGID,
			NewSHA256: raw.SHA256, NewMode: raw.Mode, NewUID: raw.OwnerUID, NewGID: raw.OwnerGID,
		}
		if item.ExpectedExists {
			if len(item.ExpectedSHA256) != 64 {
				return nil, nil, fail("INVALID_ARGUMENT", "existing patch target requires expected_sha256", map[string]any{"path": path})
			}
			if item.ExpectedMode == nil || item.ExpectedUID == nil || item.ExpectedGID == nil {
				return nil, nil, fail("INVALID_ARGUMENT", "existing patch target requires expected mode and ownership", map[string]any{"path": path})
			}
			item.BackupPath = filepath.Join(filepath.Dir(path), fmt.Sprintf(".agentdock-patch-backup-%s-%d", transactionID, index))
		}
		if item.NewExists {
			if raw.Content == nil {
				return nil, nil, fail("INVALID_ARGUMENT", "new patch content must be UTF-8 text", map[string]any{"path": path})
			}
			payload := []byte(*raw.Content)
			if len(payload) > maxTextFileBytes {
				return nil, nil, fail("FILE_TOO_LARGE", "new patch content exceeds the text file input limit", map[string]any{"path": path})
			}
			sum := sha256.Sum256(payload)
			if item.NewSHA256 != hex.EncodeToString(sum[:]) {
				return nil, nil, fail("INVALID_ARGUMENT", "new patch content hash does not match request", map[string]any{"path": path})
			}
			if item.NewMode == nil {
				return nil, nil, fail("INVALID_ARGUMENT", "new patch content requires mode", map[string]any{"path": path})
			}
			if (item.NewUID == nil) != (item.NewGID == nil) {
				return nil, nil, fail("INVALID_ARGUMENT", "owner_uid and owner_gid must be provided together", map[string]any{"path": path})
			}
			item.Content = *raw.Content
			item.TempPath = filepath.Join(filepath.Dir(path), fmt.Sprintf(".agentdock-patch-write-%s-%d", transactionID, index))
			missing, err := missingParentDirectories(path)
			if err != nil {
				return nil, nil, err
			}
			for _, directory := range missing {
				if _, exists := createdSeen[directory]; !exists {
					createdSeen[directory] = struct{}{}
					createdDirs = append(createdDirs, directory)
				}
			}
		}
		items = append(items, item)
	}
	sort.Slice(createdDirs, func(i, j int) bool {
		leftDepth := strings.Count(filepath.Clean(createdDirs[i]), string(filepath.Separator))
		rightDepth := strings.Count(filepath.Clean(createdDirs[j]), string(filepath.Separator))
		if leftDepth != rightDepth {
			return leftDepth < rightDepth
		}
		return createdDirs[i] < createdDirs[j]
	})
	return items, createdDirs, nil
}

func verifyTransactionPreflight(items []transactionItem) error {
	for _, item := range items {
		snapshot, err := pathSnapshot(item.Path)
		if err != nil {
			return err
		}
		if !item.ExpectedExists {
			if snapshot != nil {
				return fail("PATCH_CONFLICT", "patch target was created concurrently", map[string]any{"path": item.Path})
			}
			continue
		}
		if !snapshotMatches(snapshot, item.ExpectedSHA256, item.ExpectedMode, item.ExpectedUID, item.ExpectedGID) {
			return fail("PATCH_CONFLICT", "patch target changed before commit", map[string]any{"path": item.Path})
		}
	}
	return nil
}

func createTransactionTemp(item transactionItem) error {
	if !item.NewExists {
		return nil
	}
	if _, err := checkedPath(item.TempPath, true); err != nil {
		return err
	}
	file, err := os.OpenFile(item.TempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if err := unix.Fchmod(int(file.Fd()), uint32(*item.NewMode)); err != nil {
		return err
	}
	if item.NewUID != nil {
		if err := unix.Fchown(int(file.Fd()), *item.NewUID, *item.NewGID); err != nil {
			if errors.Is(err, unix.EPERM) {
				return fail("OWNERSHIP_CHANGE_BLOCKED", "patch transaction could not preserve file ownership", map[string]any{
					"path": item.Path, "owner_uid": *item.NewUID, "owner_gid": *item.NewGID, "current_uid": os.Geteuid(), "current_gid": os.Getegid(),
				})
			}
			return err
		}
	}
	if _, err := file.Write([]byte(item.Content)); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	return nil
}

func verifyBackup(item transactionItem) error {
	snapshot, err := pathSnapshot(item.BackupPath)
	if err != nil {
		return err
	}
	if !snapshotMatches(snapshot, item.ExpectedSHA256, item.ExpectedMode, item.ExpectedUID, item.ExpectedGID) {
		return fail("PATCH_CONFLICT", "patch target changed while commit was starting", map[string]any{"path": item.Path})
	}
	return nil
}

func newTargetMatches(item transactionItem) (bool, error) {
	snapshot, err := pathSnapshot(item.Path)
	if err != nil {
		return false, err
	}
	return snapshotMatches(snapshot, item.NewSHA256, item.NewMode, item.NewUID, item.NewGID), nil
}

func originalTargetMatches(item transactionItem) (bool, error) {
	snapshot, err := pathSnapshot(item.Path)
	if err != nil {
		return false, err
	}
	return snapshotMatches(snapshot, item.ExpectedSHA256, item.ExpectedMode, item.ExpectedUID, item.ExpectedGID), nil
}

func pathExistsLexically(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func rollbackTransaction(journal transactionJournal, ops transactionOps) []string {
	rollbackErrors := make([]string, 0)
	for index := len(journal.Items) - 1; index >= 0; index-- {
		item := journal.Items[index]
		backupExists := item.BackupPath != "" && pathExistsLexically(item.BackupPath)
		targetExists := pathExistsLexically(item.Path)

		if item.ExpectedExists && !backupExists {
			// 该 item 尚未 backup；当前位置仍属于事务前世界，不能按 hash 擅自删除。
			if !targetExists {
				rollbackErrors = append(rollbackErrors, "original target and backup are both missing: "+item.Path)
			}
		} else if targetExists {
			originalUnchanged := false
			installedUnchanged := false
			if item.ExpectedExists && backupExists {
				originalUnchanged, _ = originalTargetMatches(item)
			}
			if item.NewExists {
				installedUnchanged, _ = newTargetMatches(item)
			}
			switch {
			case originalUnchanged:
				// link+unlink fallback 在 unlink 前崩溃会留下同一 inode 的 target+backup。
				if err := os.Remove(item.BackupPath); err != nil {
					rollbackErrors = append(rollbackErrors, fmt.Sprintf("remove duplicate patch backup %s: %v", item.BackupPath, err))
				} else if err := ops.fsyncDirStrict(filepath.Dir(item.Path)); err != nil {
					rollbackErrors = append(rollbackErrors, fmt.Sprintf("fsync patch directory %s: %v", filepath.Dir(item.Path), err))
				} else {
					backupExists = false
				}
			case installedUnchanged:
				if err := os.Remove(item.Path); err != nil {
					rollbackErrors = append(rollbackErrors, fmt.Sprintf("remove partially installed %s: %v", item.Path, err))
				} else if err := ops.fsyncDirStrict(filepath.Dir(item.Path)); err != nil {
					rollbackErrors = append(rollbackErrors, fmt.Sprintf("fsync patch directory %s: %v", filepath.Dir(item.Path), err))
				} else {
					targetExists = false
				}
			default:
				rollbackErrors = append(rollbackErrors, "patched target changed during rollback; preserving current file: "+item.Path)
			}
		}

		if backupExists && !targetExists {
			// backup 可能包含 preflight 后的外部改动。只要求它仍是普通文件，然后原位恢复；绝不覆盖并发新建 target。
			if _, err := pathSnapshot(item.BackupPath); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Sprintf("restore patch backup for %s: %v", item.Path, err))
			} else if err := ops.renameNoReplace(item.BackupPath, item.Path); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Sprintf("restore patch backup for %s: %v", item.Path, err))
			} else if err := ops.fsyncDirStrict(filepath.Dir(item.Path)); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Sprintf("fsync restored patch directory %s: %v", filepath.Dir(item.Path), err))
			}
		}

		if item.TempPath != "" {
			if err := os.Remove(item.TempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				rollbackErrors = append(rollbackErrors, fmt.Sprintf("remove patch temp %s: %v", item.TempPath, err))
			}
		}
	}
	for index := len(journal.CreatedDirs) - 1; index >= 0; index-- {
		directory := journal.CreatedDirs[index]
		if err := os.Remove(directory); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				rollbackErrors = append(rollbackErrors, fmt.Sprintf("remove transaction-created directory %s: %v", directory, err))
			}
			continue
		}
		if err := ops.fsyncDirStrict(filepath.Dir(directory)); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Sprintf("fsync transaction-created directory parent %s: %v", filepath.Dir(directory), err))
		}
	}
	return rollbackErrors
}

func cleanupCommittedTransaction(journal transactionJournal, ops transactionOps) []string {
	cleanupErrors := make([]string, 0)
	affectedDirs := map[string]struct{}{}
	for _, item := range journal.Items {
		affectedDirs[filepath.Dir(item.Path)] = struct{}{}
		for _, candidate := range []string{item.BackupPath, item.TempPath} {
			if candidate == "" {
				continue
			}
			if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, fmt.Sprintf("remove committed transaction artifact %s: %v", candidate, err))
			}
		}
	}
	directories := make([]string, 0, len(affectedDirs))
	for directory := range affectedDirs {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	for _, directory := range directories {
		if err := ops.fsyncDirStrict(directory); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("fsync committed transaction directory %s: %v", directory, err))
		}
	}
	return cleanupErrors
}

func recoverTransactionJournals(stateDir string, ops transactionOps) (int, error) {
	entries, err := os.ReadDir(stateDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	recovered := 0
	for _, name := range names {
		journalPath := filepath.Join(stateDir, name)
		data, err := os.ReadFile(journalPath)
		if err != nil {
			return recovered, fail("PATCH_RECOVERY_REQUIRED", "cannot read WSL patch transaction journal", map[string]any{"journal": journalPath, "reason": err.Error()})
		}
		var journal transactionJournal
		if err := json.Unmarshal(data, &journal); err != nil {
			return recovered, fail("PATCH_RECOVERY_REQUIRED", "cannot read WSL patch transaction journal", map[string]any{"journal": journalPath, "reason": err.Error()})
		}
		if journal.Version != 1 {
			return recovered, fail("PATCH_RECOVERY_REQUIRED", "unsupported WSL patch transaction journal version", map[string]any{"journal": journalPath})
		}
		var recoveryErrors []string
		if journal.Phase == "committed" {
			recoveryErrors = ops.cleanupCommitted(journal, ops)
		} else {
			recoveryErrors = rollbackTransaction(journal, ops)
		}
		if len(recoveryErrors) > 0 {
			return recovered, fail("PATCH_RECOVERY_REQUIRED", "WSL patch transaction recovery is incomplete", map[string]any{"journal": journalPath, "errors": recoveryErrors})
		}
		if err := os.Remove(journalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return recovered, err
		}
		if err := ops.fsyncDirStrict(stateDir); err != nil {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}

func ensureDirectory(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fail("PATH_NOT_FOUND", "WSL path does not exist", map[string]any{"path": path})
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fail("NOT_A_DIRECTORY", "WSL path is not a directory", map[string]any{"path": path})
	}
	return nil
}

func patchTransaction(req *Request, ops transactionOps) (*Response, error) {
	workdir, err := checkedPath(req.Workdir, false)
	if err != nil {
		return nil, err
	}
	if err := ensureDirectory(workdir); err != nil {
		return nil, err
	}
	stateDir, err := transactionStateDir(workdir)
	if err != nil {
		return nil, err
	}
	lock, err := acquireTransactionLock(stateDir)
	if err != nil {
		return nil, err
	}
	defer lock.Close()

	recovered, err := recoverTransactionJournals(stateDir, ops)
	if err != nil {
		return nil, err
	}
	transactionID, err := randomTransactionID()
	if err != nil {
		return nil, err
	}
	items, plannedDirs, err := normalizeTransactionChanges(req, transactionID)
	if err != nil {
		return nil, err
	}
	if err := verifyTransactionPreflight(items); err != nil {
		return nil, err
	}

	journalPath := filepath.Join(stateDir, transactionID+".json")
	journal := transactionJournal{
		Version: 1, TransactionID: transactionID, Workdir: workdir, Phase: "preparing", CreatedDirs: []string{}, Items: items,
	}
	// 第一个 durable journal 写入失败时还没有任何 destructive mutation；保留 journal 供下一次 recovery 收尾。
	if err := writeJSONAtomic(journalPath, journal, ops); err != nil {
		return nil, err
	}

	commitErr := func() error {
		for _, directory := range plannedDirs {
			if err := os.Mkdir(directory, 0o755); err != nil {
				if !errors.Is(err, os.ErrExist) {
					return err
				}
				info, statErr := os.Lstat(directory)
				if statErr != nil {
					return statErr
				}
				if info.Mode()&os.ModeSymlink != 0 {
					return fail("SYMLINK_NOT_ALLOWED", "file_edit does not allow symlink path components", map[string]any{"path": directory})
				}
				if !info.IsDir() {
					return fail("NOT_A_DIRECTORY", "patch parent is not a directory", map[string]any{"path": directory})
				}
				continue
			}
			journal.CreatedDirs = append(journal.CreatedDirs, directory)
			if err := writeJSONAtomic(journalPath, journal, ops); err != nil {
				return err
			}
		}

		// 父目录刚创建后重新验证整个 write path，避免外部进程把父目录替换成 symlink。
		for _, item := range items {
			if _, err := checkedPath(item.Path, true); err != nil {
				return err
			}
		}
		for _, item := range items {
			if err := createTransactionTemp(item); err != nil {
				return err
			}
		}
		journal.Phase = "prepared"
		if err := writeJSONAtomic(journalPath, journal, ops); err != nil {
			return err
		}

		// destructive commit 只能在所有 target preflight、temp fsync 与 prepared journal durable 之后开始。
		for _, item := range items {
			if _, err := checkedPath(item.Path, true); err != nil {
				return err
			}
		}
		for _, item := range items {
			if !item.ExpectedExists {
				continue
			}
			if err := ops.renameNoReplace(item.Path, item.BackupPath); err != nil {
				if errors.Is(err, os.ErrExist) {
					return fail("PATCH_CONFLICT", "patch backup path already exists", map[string]any{"path": item.Path})
				}
				return err
			}
			if err := ops.fsyncDirStrict(filepath.Dir(item.Path)); err != nil {
				return err
			}
			if err := verifyBackup(item); err != nil {
				return err
			}
		}
		for _, item := range items {
			if !item.NewExists {
				continue
			}
			if err := ops.renameNoReplace(item.TempPath, item.Path); err != nil {
				if errors.Is(err, os.ErrExist) {
					return fail("PATCH_CONFLICT", "patch target was created concurrently", map[string]any{"path": item.Path})
				}
				return err
			}
			if err := ops.fsyncDirStrict(filepath.Dir(item.Path)); err != nil {
				return err
			}
		}
		for _, item := range items {
			if item.NewExists {
				matches, err := newTargetMatches(item)
				if err != nil {
					return err
				}
				if !matches {
					return fail("WRITE_VERIFICATION_FAILED", "patch transaction content verification failed", map[string]any{"path": item.Path})
				}
			} else if pathExistsLexically(item.Path) {
				return fail("PATCH_CONFLICT", "deleted patch target was recreated concurrently", map[string]any{"path": item.Path})
			}
		}
		affectedDirs := map[string]struct{}{}
		for _, item := range items {
			affectedDirs[filepath.Dir(item.Path)] = struct{}{}
		}
		directories := make([]string, 0, len(affectedDirs))
		for directory := range affectedDirs {
			directories = append(directories, directory)
		}
		sort.Strings(directories)
		for _, directory := range directories {
			if err := ops.fsyncDirStrict(directory); err != nil {
				return err
			}
		}
		journal.Phase = "committed"
		return writeJSONAtomic(journalPath, journal, ops)
	}()

	if commitErr != nil {
		rollbackErrors := rollbackTransaction(journal, ops)
		if len(rollbackErrors) > 0 {
			return nil, fail("PATCH_ROLLBACK_INCOMPLETE", "WSL patch transaction failed and rollback is incomplete", map[string]any{
				"journal": journalPath, "reason": commitErr.Error(), "rollback_errors": rollbackErrors,
			})
		}
		if err := os.Remove(journalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := ops.fsyncDirStrict(stateDir); err != nil {
			return nil, err
		}
		return nil, commitErr
	}

	cleanupErrors := ops.cleanupCommitted(journal, ops)
	cleanupPending := len(cleanupErrors) > 0
	if !cleanupPending {
		if err := os.Remove(journalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := ops.fsyncDirStrict(stateDir); err != nil {
			return nil, err
		}
	}
	return &Response{
		TransactionID: transactionID, FilesChanged: intPtr(len(items)), RecoveredTransactions: intPtr(recovered), CleanupPending: boolPtr(cleanupPending),
	}, nil
}

func recoverPatchTransactions(req *Request, ops transactionOps) (*Response, error) {
	workdir, err := checkedPath(req.Workdir, false)
	if err != nil {
		return nil, err
	}
	if err := ensureDirectory(workdir); err != nil {
		return nil, err
	}
	stateDir, err := transactionStateDir(workdir)
	if err != nil {
		return nil, err
	}
	lock, err := acquireTransactionLock(stateDir)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	recovered, err := recoverTransactionJournals(stateDir, ops)
	if err != nil {
		return nil, err
	}
	return &Response{RecoveredTransactions: intPtr(recovered)}, nil
}
