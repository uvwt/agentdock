package skill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

func (m *Manager) Remove(ctx context.Context, skill string) (RemoveResult, error) {
	skill = strings.TrimSpace(skill)
	if skill == "" {
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.input", errors.New("skill is required"))
	}

	release, err := m.State.AcquireWrite(ctx, skill)
	if err != nil {
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.lock", err)
	}
	defer release()

	destination, err := m.State.SkillPath(skill)
	if err != nil {
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.path", err)
	}
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.path", fmt.Errorf("skill %s is not installed", skill))
	}
	if err != nil {
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.path", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.path", errors.New("managed Skill path is not a regular directory"))
	}

	tombstone, err := m.State.TempPath("remove-" + skill)
	if err != nil {
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.stage", err)
	}
	if err := os.Remove(tombstone); err != nil {
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.stage", err)
	}
	if err := os.Rename(destination, tombstone); err != nil {
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.stage", err)
	}
	if err := os.RemoveAll(tombstone); err != nil {
		if restoreErr := os.Rename(tombstone, destination); restoreErr != nil {
			return RemoveResult{}, packageError(ErrUninstallFailed, "remove.cleanup", errors.Join(err, fmt.Errorf("restore removed Skill: %w", restoreErr)))
		}
		return RemoveResult{}, packageError(ErrUninstallFailed, "remove.cleanup", err)
	}
	return RemoveResult{
		Skill:                skill,
		Removed:              true,
		PreservedEnvironment: true,
		PreservedData:        true,
	}, nil
}
