package skills

import (
	"context"
	"errors"
	"strings"
)

func (m *Manager) Activate(ctx context.Context, skill, version string) (ActivateResult, error) {
	skill = strings.TrimSpace(skill)
	version = strings.TrimSpace(version)
	if skill == "" || version == "" {
		return ActivateResult{}, packageError(ErrActivateFailed, "activate.input", errors.New("skill and version are required"))
	}
	current, err := m.State.ActiveVersion(skill)
	if err != nil {
		return ActivateResult{}, packageError(ErrActivateFailed, "activate.current", err)
	}
	packageDir, err := m.State.InstalledPath(skill, version)
	if err != nil {
		return ActivateResult{}, packageError(ErrActivateFailed, "activate.resolve", err)
	}
	doc, err := LoadSkillDocument(packageDir)
	if err != nil {
		return ActivateResult{}, packageError(ErrActivateFailed, "activate.document", err)
	}
	if doc.Name != skill || doc.Version != version {
		return ActivateResult{}, packageError(ErrActivateFailed, "activate.verify", ErrDocumentIdentityMismatch{Skill: skill, Version: version, Document: doc})
	}
	if err := m.State.Activate(ctx, skill, version); err != nil {
		return ActivateResult{}, packageError(ErrActivateFailed, "activate.state", err)
	}
	return ActivateResult{Skill: skill, FromVersion: current, ToVersion: version}, nil
}
