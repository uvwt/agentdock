package skills

import (
	"context"

	"github.com/uvwt/agentdock/internal/skillstate"
)

func (m *Manager) Rollback(ctx context.Context, skill string, channel skillstate.Channel) (RollbackResult, error) {
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
	if channel == "" {
		channel = skillstate.ChannelStable
	}
	if err := m.State.Activate(ctx, skill, previous, channel); err != nil {
		return RollbackResult{}, packageError(ErrRollbackUnavailable, "rollback.activate", err)
	}
	return RollbackResult{Skill: skill, FromVersion: current, ToVersion: previous, Verified: true}, nil
}

type ErrDocumentIdentityMismatch struct {
	Skill    string
	Version  string
	Document SkillDocument
}

func (e ErrDocumentIdentityMismatch) Error() string {
	return "SKILL.md name/version do not match installed package identity"
}
