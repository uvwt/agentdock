package file

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type stagedPatchFile struct {
	Abs            string
	Display        string
	Content        *string
	Mode           os.FileMode
	Original       []byte
	OriginalExists bool
}

type preparedPatchFile struct {
	file       stagedPatchFile
	tempPath   string
	backupPath string
	installed  bool
}

type installedPatchState int

const (
	installedPatchMissing installedPatchState = iota
	installedPatchUnchanged
	installedPatchChanged
)

func commitStagedPatch(staged map[string]stagedPatchFile) error {
	return commitStagedPatchWithFileOps(staged, os.Rename, renameNoReplace)
}

func commitStagedPatchWithRename(staged map[string]stagedPatchFile, rename func(string, string) error) error {
	return commitStagedPatchWithFileOps(staged, rename, os.Link)
}

func commitStagedPatchWithFileOps(staged map[string]stagedPatchFile, rename, installNoReplace func(string, string) error) error {
	paths := make([]string, 0, len(staged))
	for path := range staged {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	prepared := make([]preparedPatchFile, 0, len(paths))
	createdDirs := make([]string, 0)
	for _, path := range paths {
		file := staged[path]
		if err := verifyPatchOriginal(file); err != nil {
			cleanupPreparedPatch(prepared, createdDirs)
			return err
		}
		if file.Content == nil {
			prepared = append(prepared, preparedPatchFile{file: file})
			continue
		}
		dirs, err := ensurePatchParent(filepath.Dir(file.Abs))
		if err != nil {
			cleanupPreparedPatch(prepared, createdDirs)
			return err
		}
		createdDirs = append(createdDirs, dirs...)
		tempPath, err := writePatchTemp(file)
		if err != nil {
			cleanupPreparedPatch(prepared, createdDirs)
			return err
		}
		prepared = append(prepared, preparedPatchFile{file: file, tempPath: tempPath})
	}

	for i := range prepared {
		item := &prepared[i]
		if item.file.OriginalExists {
			backupPath, err := reservePatchPath(filepath.Dir(item.file.Abs), ".agentdock-patch-backup-*")
			if err != nil {
				return rollbackPatch(prepared, createdDirs, rename, fmt.Errorf("reserve patch backup for %s: %w", item.file.Display, err))
			}
			if err := rename(item.file.Abs, backupPath); err != nil {
				return rollbackPatch(prepared, createdDirs, rename, fmt.Errorf("backup patch target %s: %w", item.file.Display, err))
			}
			item.backupPath = backupPath
			if err := verifyPatchBackup(*item); err != nil {
				return rollbackPatch(prepared, createdDirs, rename, err)
			}
		}
	}
	for i := range prepared {
		item := &prepared[i]
		if item.file.Content == nil {
			continue
		}
		// 备份后原路径必须仍为空；平台 no-replace rename 提供“目标存在则失败”
		// 语义，避免外部程序在提交窗口重建文件后被静默覆盖。
		if err := installNoReplace(item.tempPath, item.file.Abs); err != nil {
			return rollbackPatch(prepared, createdDirs, rename, toolErrorDetails("PATCH_CONFLICT", "patch target was created concurrently or cannot be installed without replacing an existing file", "runtime", map[string]any{"path": item.file.Display, "reason": err.Error()}))
		}
		item.installed = true
		if err := os.Remove(item.tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return rollbackPatch(prepared, createdDirs, rename, fmt.Errorf("remove installed patch temp for %s: %w", item.file.Display, err))
		}
		item.tempPath = ""
	}

	for _, item := range prepared {
		if item.backupPath != "" {
			if err := os.Remove(item.backupPath); err != nil {
				slog.Warn("remove committed patch backup failed", "path", item.backupPath, "error", err)
			}
		}
	}
	return nil
}

func verifyPatchOriginal(file stagedPatchFile) error {
	if !file.OriginalExists {
		if _, err := os.Lstat(file.Abs); err == nil {
			return toolErrorDetails("PATCH_CONFLICT", "patch target was created concurrently", "runtime", map[string]any{"path": file.Display})
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	info, content, err := readPatchFile(file.Abs)
	if err != nil {
		return toolErrorDetails("PATCH_CONFLICT", "patch target changed before commit", "runtime", map[string]any{"path": file.Display, "reason": err.Error()})
	}
	if info.Mode().Perm() != file.Mode.Perm() || !bytes.Equal(content, file.Original) {
		return toolErrorDetails("PATCH_CONFLICT", "patch target changed before commit", "runtime", map[string]any{"path": file.Display})
	}
	return nil
}

func verifyPatchBackup(item preparedPatchFile) error {
	info, content, err := readPatchFile(item.backupPath)
	if err != nil {
		return toolErrorDetails("PATCH_CONFLICT", "patch target changed while commit was starting", "runtime", map[string]any{"path": item.file.Display, "reason": err.Error()})
	}
	if info.Mode().Perm() != item.file.Mode.Perm() || !bytes.Equal(content, item.file.Original) {
		return toolErrorDetails("PATCH_CONFLICT", "patch target changed while commit was starting", "runtime", map[string]any{"path": item.file.Display})
	}
	return nil
}

func ensurePatchParent(parent string) ([]string, error) {
	missing := make([]string, 0)
	cursor := parent
	for {
		info, err := os.Stat(cursor)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("patch parent is not a directory: %s", cursor)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		missing = append(missing, cursor)
		next := filepath.Dir(cursor)
		if next == cursor {
			return nil, fmt.Errorf("patch parent directory not found: %s", parent)
		}
		cursor = next
	}
	created := make([]string, 0, len(missing))
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o755); err != nil {
			removeEmptyPatchDirs(created)
			return nil, err
		}
		created = append(created, missing[i])
	}
	return created, nil
}

func writePatchTemp(file stagedPatchFile) (path string, returnErr error) {
	temp, err := os.CreateTemp(filepath.Dir(file.Abs), ".agentdock-patch-write-*")
	if err != nil {
		return "", err
	}
	path = temp.Name()
	defer func() {
		if returnErr != nil {
			_ = temp.Close()
			_ = os.Remove(path)
		}
	}()
	if err := temp.Chmod(file.Mode.Perm()); err != nil {
		return "", err
	}
	if _, err := temp.WriteString(*file.Content); err != nil {
		return "", err
	}
	if err := temp.Sync(); err != nil {
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func reservePatchPath(dir, pattern string) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func rollbackPatch(prepared []preparedPatchFile, createdDirs []string, rename func(string, string) error, cause error) error {
	errs := []error{cause}
	for i := len(prepared) - 1; i >= 0; i-- {
		item := &prepared[i]
		canRestoreBackup := true
		if item.installed {
			switch inspectInstalledPatchFile(*item) {
			case installedPatchChanged:
				message := fmt.Sprintf("patched target %s changed during rollback; preserving current file", item.file.Display)
				if item.backupPath != "" {
					message += " and original backup at " + item.backupPath
				}
				errs = append(errs, errors.New(message))
				canRestoreBackup = false
			case installedPatchUnchanged:
				if err := os.Remove(item.file.Abs); err != nil && !errors.Is(err, os.ErrNotExist) {
					errs = append(errs, fmt.Errorf("remove partially installed %s: %w", item.file.Display, err))
					canRestoreBackup = false
				}
			case installedPatchMissing:
				// 外部删除了事务写入文件；原路径为空时可以直接恢复备份。
			}
		}
		if item.backupPath != "" && canRestoreBackup {
			if _, err := os.Lstat(item.file.Abs); err == nil {
				errs = append(errs, fmt.Errorf("cannot restore patch backup for %s without overwriting a concurrent file; original retained at %s", item.file.Display, item.backupPath))
			} else if !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("check patch restore target %s: %w", item.file.Display, err))
			} else if err := rename(item.backupPath, item.file.Abs); err != nil {
				errs = append(errs, fmt.Errorf("restore patch backup %s from %s: %w", item.file.Display, item.backupPath, err))
			} else {
				item.backupPath = ""
			}
		}
		if item.tempPath != "" {
			if err := os.Remove(item.tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("remove patch temp %s: %w", item.tempPath, err))
			}
		}
	}
	removeEmptyPatchDirs(createdDirs)
	return errors.Join(errs...)
}

func inspectInstalledPatchFile(item preparedPatchFile) installedPatchState {
	if item.file.Content == nil {
		return installedPatchChanged
	}
	file, err := os.Open(item.file.Abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return installedPatchMissing
		}
		return installedPatchChanged
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Mode().Perm() != item.file.Mode.Perm() || info.Size() != int64(len(*item.file.Content)) {
		return installedPatchChanged
	}
	actualHash := sha256.New()
	if _, err := io.Copy(actualHash, file); err != nil {
		return installedPatchChanged
	}
	expectedHash := sha256.New()
	if _, err := io.Copy(expectedHash, strings.NewReader(*item.file.Content)); err != nil {
		return installedPatchChanged
	}
	if bytes.Equal(actualHash.Sum(nil), expectedHash.Sum(nil)) {
		return installedPatchUnchanged
	}
	return installedPatchChanged
}

func cleanupPreparedPatch(prepared []preparedPatchFile, createdDirs []string) {
	for _, item := range prepared {
		if item.tempPath != "" {
			_ = os.Remove(item.tempPath)
		}
	}
	removeEmptyPatchDirs(createdDirs)
}

func removeEmptyPatchDirs(created []string) {
	for i := len(created) - 1; i >= 0; i-- {
		_ = os.Remove(created[i])
	}
}
