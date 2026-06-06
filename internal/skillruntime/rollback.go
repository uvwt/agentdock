package skillruntime

import (
	"context"
	"fmt"

	"github.com/uvwt/agentdock/internal/skillstate"
)

type Verifier interface {
	Verify(context.Context, string, string, []string) ([]VerificationResult, error)
}

type VerifierFunc func(context.Context, string, string, []string) ([]VerificationResult, error)

func (f VerifierFunc) Verify(ctx context.Context, skill, version string, checks []string) ([]VerificationResult, error) {
	return f(ctx, skill, version, checks)
}

func (r *Runtime) Rollback(ctx context.Context, skill string, channel skillstate.Channel, verifier Verifier) (RollbackResult, error) {
	current, err := r.State.ActiveVersion(skill)
	if err != nil {
		return RollbackResult{}, runtimeError(ErrRollbackUnavailable, "rollback.current", err)
	}
	previous, err := r.State.PreviousVersion(skill)
	if err != nil {
		return RollbackResult{}, runtimeError(ErrRollbackUnavailable, "rollback.previous", err)
	}
	if channel == "" {
		channel = skillstate.ChannelStable
	}
	previousPath, err := r.State.InstalledPath(skill, previous)
	if err != nil {
		return RollbackResult{}, runtimeError(ErrRollbackUnavailable, "rollback.resolve", err)
	}
	manifest, err := LoadManifest(previousPath)
	if err != nil {
		return RollbackResult{}, err
	}
	if err := r.State.Activate(ctx, skill, previous, channel); err != nil {
		return RollbackResult{}, runtimeError(ErrRollbackUnavailable, "rollback.activate", err)
	}
	result := RollbackResult{Skill: skill, FromVersion: current, ToVersion: previous}
	if verifier == nil || len(manifest.Spec.Verification) == 0 {
		result.Verified = true
		return result, nil
	}
	checks, verifyErr := verifier.Verify(ctx, skill, previous, manifest.Spec.Verification)
	result.Verification = checks
	allOK := verifyErr == nil
	for _, check := range checks {
		if !check.OK {
			allOK = false
		}
	}
	if allOK {
		result.Verified = true
		return result, nil
	}
	if restoreErr := r.State.Activate(ctx, skill, current, channel); restoreErr != nil {
		return result, runtimeError(ErrRollbackVerify, "rollback.restore", fmt.Errorf("verification failed: %v; restoring %s also failed: %w", verifyErr, current, restoreErr))
	}
	if verifyErr == nil {
		verifyErr = fmt.Errorf("one or more rollback verification checks failed")
	}
	return result, runtimeError(ErrRollbackVerify, "rollback.verify", verifyErr)
}
