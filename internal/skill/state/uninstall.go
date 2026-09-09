package state

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type UninstallResult struct {
	RemovedVersions []string
	ActiveVersion   string
}

// Uninstall 与激活共用同一个 Skill 锁，避免卸载和版本切换互相穿插。
// 删除单个版本时同步清理回滚历史；删除整个 Skill 时一并清除版本选择状态。
func (s *Store) Uninstall(ctx context.Context, skill, version string) (UninstallResult, error) {
	if err := validateIdentifier("skill", skill); err != nil {
		return UninstallResult{}, err
	}
	if version != "" {
		if err := validateIdentifier("version", version); err != nil {
			return UninstallResult{}, err
		}
	}

	release, err := s.acquire(ctx, skill)
	if err != nil {
		return UninstallResult{}, err
	}
	defer release()

	selection, err := s.load(skill)
	if err != nil {
		return UninstallResult{}, err
	}
	if version == "" {
		return s.uninstallSkillLocked(skill, selection)
	}
	return s.uninstallVersionLocked(skill, version, selection)
}

func (s *Store) uninstallVersionLocked(skill, version string, selection Selection) (UninstallResult, error) {
	if selection.ActiveVersion == version {
		return UninstallResult{}, fmt.Errorf("cannot uninstall active skill version %s", version)
	}

	packagePath, err := s.InstalledPath(skill, version)
	if err != nil {
		return UninstallResult{}, err
	}
	info, err := os.Stat(packagePath)
	if errors.Is(err, os.ErrNotExist) {
		return UninstallResult{}, fmt.Errorf("skill %s version %s is not installed", skill, version)
	}
	if err != nil {
		return UninstallResult{}, err
	}
	if !info.IsDir() {
		return UninstallResult{}, fmt.Errorf("skill %s version %s is not an installed directory", skill, version)
	}

	tombstone, err := s.uninstallTombstone("package")
	if err != nil {
		return UninstallResult{}, err
	}
	if err := os.Rename(packagePath, tombstone); err != nil {
		return UninstallResult{}, fmt.Errorf("stage skill version removal: %w", err)
	}

	next := selection
	var historyChanged bool
	next.History, historyChanged = removeVersion(next.History, version)
	if historyChanged {
		next.UpdatedAt = time.Now().UTC()
		if err := s.persistSelectionAfterUninstall(skill, next); err != nil {
			rollbackErr := os.Rename(tombstone, packagePath)
			return UninstallResult{}, errors.Join(err, wrapRollbackError(rollbackErr))
		}
	}

	cleanupUninstallTombstone(tombstone)
	// 这里只做尽力清理：仍有其他版本时目录本来就应该保留；若这是最后一个
	// 未激活版本，则顺手移除空目录，避免 ListSkills 返回空 Skill。
	_ = os.Remove(filepath.Dir(packagePath))
	return UninstallResult{RemovedVersions: []string{version}, ActiveVersion: selection.ActiveVersion}, nil
}

func (s *Store) uninstallSkillLocked(skill string, selection Selection) (UninstallResult, error) {
	versions, err := s.ListVersions(skill)
	if err != nil {
		return UninstallResult{}, err
	}
	if len(versions) == 0 {
		return UninstallResult{}, fmt.Errorf("skill %s is not installed", skill)
	}
	sort.Strings(versions)

	// 整体卸载先暂存 selection state，再暂存安装目录。这样并发只读请求最多
	// 会看到“当前无激活版本”，不会短暂拿到一个已经不存在的 active 路径。
	statePath := filepath.Join(s.root, "state", skill+".json")
	stateTombstone, err := s.uninstallTombstone("state")
	if err != nil {
		return UninstallResult{}, err
	}
	stateMoved := false
	if err := os.Rename(statePath, stateTombstone); err == nil {
		stateMoved = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return UninstallResult{}, fmt.Errorf("stage skill state removal: %w", err)
	}

	packagePath := filepath.Join(s.root, "installed", skill)
	packageTombstone, err := s.uninstallTombstone("package")
	if err != nil {
		return UninstallResult{}, errors.Join(err, restoreStagedState(stateMoved, stateTombstone, statePath))
	}
	if err := os.Rename(packagePath, packageTombstone); err != nil {
		return UninstallResult{}, errors.Join(fmt.Errorf("stage skill removal: %w", err), restoreStagedState(stateMoved, stateTombstone, statePath))
	}

	cleanupUninstallTombstone(packageTombstone)
	if stateMoved {
		cleanupUninstallTombstone(stateTombstone)
	}
	return UninstallResult{RemovedVersions: versions, ActiveVersion: selection.ActiveVersion}, nil
}

func (s *Store) persistSelectionAfterUninstall(skill string, selection Selection) error {
	if selection.ActiveVersion == "" && len(selection.History) == 0 {
		path := filepath.Join(s.root, "state", skill+".json")
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove empty skill state: %w", err)
		}
		return nil
	}
	if err := s.save(skill, selection); err != nil {
		return fmt.Errorf("update skill state after uninstall: %w", err)
	}
	return nil
}

func (s *Store) uninstallTombstone(kind string) (string, error) {
	owner, err := newLockOwner()
	if err != nil {
		return "", fmt.Errorf("create uninstall tombstone: %w", err)
	}
	return filepath.Join(s.root, "tmp", "uninstall-"+kind+"-"+owner), nil
}

func cleanupUninstallTombstone(path string) {
	if err := os.RemoveAll(path); err != nil {
		slog.Warn("cleanup Skill uninstall tombstone failed", "path", path, "error", err)
	}
}

func restoreStagedState(moved bool, tombstone, destination string) error {
	if !moved {
		return nil
	}
	if err := os.Rename(tombstone, destination); err != nil {
		return fmt.Errorf("restore staged Skill state: %w", err)
	}
	return nil
}

func wrapRollbackError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("restore staged Skill package: %w", err)
}

func removeVersion(history []string, version string) ([]string, bool) {
	filtered := make([]string, 0, len(history))
	changed := false
	for _, item := range history {
		if item == version {
			changed = true
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, changed
}
