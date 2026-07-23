package skill

import (
	"context"
)

func (m *Manager) Rollback(ctx context.Context, skill string) (RollbackResult, error) {
	current, err := m.State.ActiveVersion(skill)
	if err != nil {
		return RollbackResult{}, packageError(ErrRollbackUnavailable, "rollback.current", err)
	}
	previous, err := m.State.PreviousVersion(skill)
	if err != nil {
		return RollbackResult{}, packageError(ErrRollbackUnavailable, "rollback.previous", err)
	}
	previousPath, err := m.State.InstalledPath(skill, previous)
	if err != nil {
		return RollbackResult{}, packageError(ErrRollbackUnavailable, "rollback.resolve", err)
	}
	doc, err := LoadSkillDocument(previousPath)
	if err != nil {
		return RollbackResult{}, err
	}
	if doc.Name != skill || doc.Version != previous {
		return RollbackResult{}, packageError(ErrRollbackUnavailable, "rollback.verify", ErrDocumentIdentityMismatch{Skill: skill, Version: previous, Document: doc})
	}
	if err := m.State.Activate(ctx, skill, previous); err != nil {
		return RollbackResult{}, packageError(ErrRollbackUnavailable, "rollback.activate", err)
	}
	return RollbackResult{Skill: skill, FromVersion: current, ToVersion: previous, Verified: true}, nil
}
