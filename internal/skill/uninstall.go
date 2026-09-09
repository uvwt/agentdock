package skill

import (
	"context"
	"errors"
	"strings"
)

func (m *Manager) Uninstall(ctx context.Context, skill, version string) (UninstallResult, error) {
	skill = strings.TrimSpace(skill)
	version = strings.TrimSpace(version)
	if skill == "" {
		return UninstallResult{}, packageError(ErrUninstallFailed, "uninstall.input", errors.New("skill is required"))
	}

	bundled, err := m.State.IsBundled(skill)
	if err != nil {
		return UninstallResult{}, packageError(ErrUninstallFailed, "uninstall.bundled", err)
	}
	if bundled {
		return UninstallResult{}, packageError(ErrUninstallFailed, "uninstall.bundled", errors.New("bundled Skill cannot be uninstalled"))
	}

	removed, err := m.State.Uninstall(ctx, skill, version)
	if err != nil {
		return UninstallResult{}, packageError(ErrUninstallFailed, "uninstall.state", err)
	}
	return UninstallResult{
		Skill:                skill,
		RemovedVersions:      removed.RemovedVersions,
		ActiveVersion:        removed.ActiveVersion,
		PreservedEnvironment: true,
		PreservedData:        true,
	}, nil
}
