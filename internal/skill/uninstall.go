package skill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

func (m *Manager) Remove(ctx context.Context, skill string) (RemoveResult, error) {
	return m.remove(ctx, skill, false, nil)
}

// RemoveWithPurge removes the managed package and runs purge while the same
// Skill lifecycle write lock is still held. It is intentionally idempotent:
// when the package is already absent, purge still runs so preserved env/data
// from an earlier keep removal can be cleaned safely.
func (m *Manager) RemoveWithPurge(ctx context.Context, skill string, purge func() error) (RemoveResult, error) {
	if purge == nil {
		return RemoveResult{}, packageError(ErrPurgeFailed, "purge.callback", errors.New("purge callback is required"))
	}
	return m.remove(ctx, skill, true, purge)
}

func (m *Manager) remove(ctx context.Context, skill string, allowMissing bool, purge func() error) (RemoveResult, error) {
	skill = strings.TrimSpace(skill)
	if skill == "" {
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.input", errors.New("skill is required"))
	}

	release, err := m.State.AcquireWrite(ctx, skill)
	if err != nil {
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.lock", err)
	}
	defer release()
	if err := m.recoverSwapLocked(skill); err != nil {
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.recover", err)
	}

	result := RemoveResult{
		Skill:                skill,
		Removed:              false,
		PreservedEnvironment: purge == nil,
		PreservedData:        purge == nil,
	}
	destination, err := m.State.SkillPath(skill)
	if err != nil {
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.path", err)
	}
	info, err := os.Lstat(destination)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if !allowMissing {
			return RemoveResult{}, packageError(ErrUninstallFailed, "remove.path", fmt.Errorf("skill %s is not installed", skill))
		}
	case err != nil:
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.path", err)
	default:
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return RemoveResult{}, packageError(ErrUninstallFailed, "remove.path", errors.New("managed Skill path is not a regular directory"))
		}
		if err := m.removePackageLocked(skill, destination); err != nil {
			return RemoveResult{}, err
		}
		result.Removed = true
	}

	if purge != nil {
		if err := purge(); err != nil {
			return result, packageError(ErrPurgeFailed, "purge.state", err)
		}
		result.PreservedEnvironment = false
		result.PreservedData = false
	}
	return result, nil
}

func (m *Manager) removePackageLocked(skill, destination string) error {
	tombstone, err := m.State.TempPath("remove-" + skill)
	if err != nil {
		return packageError(ErrUninstallFailed, "remove.stage", err)
	}
	if err := os.Remove(tombstone); err != nil {
		return packageError(ErrUninstallFailed, "remove.stage", err)
	}
	if err := os.Rename(destination, tombstone); err != nil {
		return packageError(ErrUninstallFailed, "remove.stage", err)
	}
	if err := os.RemoveAll(tombstone); err != nil {
		if restoreErr := os.Rename(tombstone, destination); restoreErr != nil {
			return packageError(ErrUninstallFailed, "remove.cleanup", errors.Join(err, fmt.Errorf("restore removed Skill: %w", restoreErr)))
		}
		return packageError(ErrUninstallFailed, "remove.cleanup", err)
	}
	return nil
}
