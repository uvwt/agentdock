package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	skillstate "github.com/uvwt/agentdock/internal/skill/state"
)

func TestRestartRecoversPreparedSwapAfterPreviousMoved(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "skills")
	state, err := skillstate.New(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(state)
	if err != nil {
		t.Fatal(err)
	}
	sourceV1 := writeSkillSource(t, "demo-skill", "Previous", nil)
	installed, err := manager.Install(context.Background(), InstallRequest{Source: sourceV1})
	if err != nil {
		t.Fatal(err)
	}

	sourceV2 := writeSkillSource(t, "demo-skill", "Candidate", nil)
	candidateDigest, err := DigestPackageContent(sourceV2)
	if err != nil {
		t.Fatal(err)
	}
	transaction := newSkillSwapTransaction("demo-skill", installed.ContentDigest, candidateDigest)
	if err := state.SaveSwapTransaction(transaction); err != nil {
		t.Fatal(err)
	}
	backup, err := state.SwapBackupPath("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(installed.Path, backup); err != nil {
		t.Fatal(err)
	}

	restartedState, err := skillstate.New(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(restartedState); err != nil {
		t.Fatal(err)
	}
	digest, exists, err := installedContentDigest(installed.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || digest != installed.ContentDigest {
		t.Fatalf("restart did not restore Previous: exists=%v digest=%s want=%s", exists, digest, installed.ContentDigest)
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restart left Skill replacement backup: %v", err)
	}
	if _, err := restartedState.LoadSwapTransaction("demo-skill"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restart left prepared journal: %v", err)
	}
}

func TestRestartRollsBackPreparedSwapAfterCandidateRename(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "skills")
	state, err := skillstate.New(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(state)
	if err != nil {
		t.Fatal(err)
	}
	sourceV1 := writeSkillSource(t, "demo-skill", "Previous", nil)
	installed, err := manager.Install(context.Background(), InstallRequest{Source: sourceV1})
	if err != nil {
		t.Fatal(err)
	}
	sourceV2 := writeSkillSource(t, "demo-skill", "Candidate", map[string]string{"candidate.txt": "v2"})
	candidateDigest, err := DigestPackageContent(sourceV2)
	if err != nil {
		t.Fatal(err)
	}
	transaction := newSkillSwapTransaction("demo-skill", installed.ContentDigest, candidateDigest)
	if err := state.SaveSwapTransaction(transaction); err != nil {
		t.Fatal(err)
	}
	backup, err := state.SwapBackupPath("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(installed.Path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(sourceV2, installed.Path); err != nil {
		t.Fatal(err)
	}

	restartedState, err := skillstate.New(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(restartedState); err != nil {
		t.Fatal(err)
	}
	digest, exists, err := installedContentDigest(installed.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || digest != installed.ContentDigest {
		t.Fatalf("restart kept uncommitted candidate: exists=%v digest=%s want=%s", exists, digest, installed.ContentDigest)
	}
	if _, err := os.Stat(filepath.Join(installed.Path, "candidate.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restart left uncommitted candidate file: %v", err)
	}
}

func TestRestartFinishesPublishedSwapForward(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "skills")
	state, err := skillstate.New(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(state)
	if err != nil {
		t.Fatal(err)
	}
	sourceV1 := writeSkillSource(t, "demo-skill", "Previous", nil)
	installed, err := manager.Install(context.Background(), InstallRequest{Source: sourceV1})
	if err != nil {
		t.Fatal(err)
	}
	sourceV2 := writeSkillSource(t, "demo-skill", "Candidate", map[string]string{"candidate.txt": "v2"})
	candidateDigest, err := DigestPackageContent(sourceV2)
	if err != nil {
		t.Fatal(err)
	}
	transaction := newSkillSwapTransaction("demo-skill", installed.ContentDigest, candidateDigest)
	if err := state.SaveSwapTransaction(transaction); err != nil {
		t.Fatal(err)
	}
	backup, err := state.SwapBackupPath("demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(installed.Path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(sourceV2, installed.Path); err != nil {
		t.Fatal(err)
	}
	transaction.Phase = "candidate_published"
	if err := state.SaveSwapTransaction(transaction); err != nil {
		t.Fatal(err)
	}

	restartedState, err := skillstate.New(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(restartedState); err != nil {
		t.Fatal(err)
	}
	digest, exists, err := installedContentDigest(installed.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || digest != candidateDigest {
		t.Fatalf("restart failed to keep committed candidate: exists=%v digest=%s want=%s", exists, digest, candidateDigest)
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restart left committed backup: %v", err)
	}
	if _, err := restartedState.LoadSwapTransaction("demo-skill"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restart left committed journal: %v", err)
	}
}

func TestSecondSkillManagerDoesNotRecoverLiveSwap(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "skills")
	state, err := skillstate.New(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(state)
	if err != nil {
		t.Fatal(err)
	}
	source := writeSkillSource(t, "demo-skill", "Previous", nil)
	installed, err := manager.Install(context.Background(), InstallRequest{Source: source})
	if err != nil {
		t.Fatal(err)
	}
	release, err := state.AcquireWrite(context.Background(), "demo-skill")
	if err != nil {
		t.Fatal(err)
	}
	transaction := newSkillSwapTransaction("demo-skill", installed.ContentDigest, "sha256:"+string(make([]byte, 64)))
	transaction.CandidateDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := state.SaveSwapTransaction(transaction); err != nil {
		release()
		t.Fatal(err)
	}

	secondState, err := skillstate.New(stateRoot)
	if err != nil {
		release()
		t.Fatal(err)
	}
	if _, err := New(secondState); err != nil {
		release()
		t.Fatal(err)
	}
	if _, err := secondState.LoadSwapTransaction("demo-skill"); err != nil {
		release()
		t.Fatalf("second Manager recovered a live swap: %v", err)
	}
	digest, exists, err := installedContentDigest(installed.Path)
	if err != nil {
		release()
		t.Fatal(err)
	}
	if !exists || digest != installed.ContentDigest {
		release()
		t.Fatalf("second Manager changed live current Skill: exists=%v digest=%s", exists, digest)
	}
	release()

	thirdState, err := skillstate.New(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(thirdState); err != nil {
		t.Fatal(err)
	}
	if _, err := thirdState.LoadSwapTransaction("demo-skill"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("post-release recovery left journal: %v", err)
	}
}
