//go:build linux

package wslfilehelper

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func existingTransactionChange(t *testing.T, path string, content *string) ChangeRequest {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mode, uid, gid := statIdentity(info)
	expected := sha256.Sum256(data)
	change := ChangeRequest{
		Path: path, ExpectedExists: boolPtr(true), NewExists: boolPtr(content != nil),
		ExpectedSHA256: hex.EncodeToString(expected[:]), ExpectedMode: intPtr(mode), ExpectedUID: intPtr(uid), ExpectedGID: intPtr(gid),
	}
	if content != nil {
		next := sha256.Sum256([]byte(*content))
		change.Content = content
		change.SHA256 = hex.EncodeToString(next[:])
		change.Mode = intPtr(mode)
		change.OwnerUID = intPtr(uid)
		change.OwnerGID = intPtr(gid)
	}
	return change
}

func newTransactionChange(path, content string, mode int) ChangeRequest {
	sum := sha256.Sum256([]byte(content))
	return ChangeRequest{
		Path: path, ExpectedExists: boolPtr(false), NewExists: boolPtr(true), Content: &content,
		SHA256: hex.EncodeToString(sum[:]), Mode: intPtr(mode),
	}
}

func withTransactionState(t *testing.T) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", state)
	return state
}

func TestWSLPatchTransactionCommitsMultipleFiles(t *testing.T) {
	root := t.TempDir()
	withTransactionState(t)
	updatePath := filepath.Join(root, "update.txt")
	deletePath := filepath.Join(root, "delete.txt")
	moveSource := filepath.Join(root, "move-source.txt")
	moveDestination := filepath.Join(root, "nested", "move-destination.txt")
	addPath := filepath.Join(root, "nested", "add.txt")
	mustWriteFile(t, updatePath, "old update\n", 0o640)
	mustWriteFile(t, deletePath, "delete me\n", 0o640)
	mustWriteFile(t, moveSource, "move me\n", 0o640)

	updated := "new update\n"
	moved := "move me\n"
	response, err := Dispatch(&Request{
		Action: "patch_transaction", Workdir: root,
		Changes: []ChangeRequest{
			existingTransactionChange(t, updatePath, &updated),
			existingTransactionChange(t, deletePath, nil),
			existingTransactionChange(t, moveSource, nil),
			newTransactionChange(moveDestination, moved, 0o640),
			newTransactionChange(addPath, "added\n", 0o644),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.FilesChanged == nil || *response.FilesChanged != 5 || response.CleanupPending == nil || *response.CleanupPending {
		t.Fatalf("transaction response = %#v", response)
	}
	for path, want := range map[string]string{updatePath: updated, moveDestination: moved, addPath: "added\n"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s content=%q err=%v want=%q", path, got, err, want)
		}
	}
	for _, path := range []string{deletePath, moveSource} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists: %v", path, err)
		}
	}
	assertNoTransactionArtifacts(t, root)
}

func TestWSLPatchTransactionConflictAbortsBeforeCommit(t *testing.T) {
	root := t.TempDir()
	withTransactionState(t)
	first := filepath.Join(root, "first.txt")
	second := filepath.Join(root, "second.txt")
	mustWriteFile(t, first, "first old\n", 0o600)
	mustWriteFile(t, second, "second old\n", 0o600)
	firstNew, secondNew := "first new\n", "second new\n"
	firstChange := existingTransactionChange(t, first, &firstNew)
	secondChange := existingTransactionChange(t, second, &secondNew)
	secondChange.ExpectedSHA256 = strings.Repeat("0", 64)

	_, err := Dispatch(&Request{Action: "patch_transaction", Workdir: root, Changes: []ChangeRequest{firstChange, secondChange}})
	requireFailureCode(t, err, "PATCH_CONFLICT")
	for path, want := range map[string]string{first: "first old\n", second: "second old\n"} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || string(got) != want {
			t.Fatalf("%s changed despite preflight conflict: %q err=%v", path, got, readErr)
		}
	}
	assertNoTransactionArtifacts(t, root)
}

func TestWSLPatchTransactionRollsBackPartialInstall(t *testing.T) {
	root := t.TempDir()
	withTransactionState(t)
	first := filepath.Join(root, "first.txt")
	second := filepath.Join(root, "second.txt")
	mustWriteFile(t, first, "first old\n", 0o600)
	mustWriteFile(t, second, "second old\n", 0o600)
	firstNew, secondNew := "first new\n", "second new\n"
	req := &Request{Action: "patch_transaction", Workdir: root, Changes: []ChangeRequest{
		existingTransactionChange(t, first, &firstNew), existingTransactionChange(t, second, &secondNew),
	}}
	ops := defaultTransactionOps()
	original := ops.renameNoReplace
	installs := 0
	ops.renameNoReplace = func(source, destination string) error {
		if strings.HasPrefix(filepath.Base(source), ".agentdock-patch-write-") {
			installs++
			if installs == 2 {
				return errors.New("injected install failure")
			}
		}
		return original(source, destination)
	}
	if _, err := dispatchWithOps(req, ops); err == nil {
		t.Fatal("expected injected install failure")
	}
	for path, want := range map[string]string{first: "first old\n", second: "second old\n"} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || string(got) != want {
			t.Fatalf("%s was not rolled back: %q err=%v", path, got, readErr)
		}
	}
	assertNoTransactionArtifacts(t, root)
}

func TestWSLPatchTransactionRestoresExternalChangeDetectedDuringBackup(t *testing.T) {
	root := t.TempDir()
	withTransactionState(t)
	target := filepath.Join(root, "target.txt")
	mustWriteFile(t, target, "original\n", 0o600)
	updated := "transaction\n"
	req := &Request{Action: "patch_transaction", Workdir: root, Changes: []ChangeRequest{existingTransactionChange(t, target, &updated)}}
	ops := defaultTransactionOps()
	original := ops.renameNoReplace
	ops.renameNoReplace = func(source, destination string) error {
		if strings.HasPrefix(filepath.Base(destination), ".agentdock-patch-backup-") {
			if err := os.WriteFile(source, []byte("external\n"), 0o600); err != nil {
				return err
			}
		}
		return original(source, destination)
	}
	_, err := dispatchWithOps(req, ops)
	requireFailureCode(t, err, "PATCH_CONFLICT")
	got, readErr := os.ReadFile(target)
	if readErr != nil || string(got) != "external\n" {
		t.Fatalf("external change was not restored: %q err=%v", got, readErr)
	}
	assertNoTransactionArtifacts(t, root)
}

func TestWSLPatchTransactionPreservesConcurrentTarget(t *testing.T) {
	root := t.TempDir()
	withTransactionState(t)
	existing := filepath.Join(root, "existing.txt")
	concurrent := filepath.Join(root, "concurrent.txt")
	mustWriteFile(t, existing, "old\n", 0o600)
	updated := "new\n"
	req := &Request{Action: "patch_transaction", Workdir: root, Changes: []ChangeRequest{
		existingTransactionChange(t, existing, &updated), newTransactionChange(concurrent, "transaction\n", 0o644),
	}}
	ops := defaultTransactionOps()
	original := ops.renameNoReplace
	ops.renameNoReplace = func(source, destination string) error {
		if strings.HasPrefix(filepath.Base(source), ".agentdock-patch-write-") && destination == concurrent {
			if err := os.WriteFile(destination, []byte("external\n"), 0o644); err != nil {
				return err
			}
		}
		return original(source, destination)
	}
	_, err := dispatchWithOps(req, ops)
	requireFailureCode(t, err, "PATCH_ROLLBACK_INCOMPLETE")
	if got, readErr := os.ReadFile(existing); readErr != nil || string(got) != "old\n" {
		t.Fatalf("existing target not restored: %q err=%v", got, readErr)
	}
	if got, readErr := os.ReadFile(concurrent); readErr != nil || string(got) != "external\n" {
		t.Fatalf("concurrent target was not preserved: %q err=%v", got, readErr)
	}
}

func TestWSLPatchTransactionRecoversJournalAfterDirectoryFsyncFailure(t *testing.T) {
	root := t.TempDir()
	withTransactionState(t)
	target := filepath.Join(root, "target.txt")
	mustWriteFile(t, target, "old\n", 0o600)
	updated := "new\n"
	stateDir, err := transactionStateDir(root)
	if err != nil {
		t.Fatal(err)
	}
	req := &Request{Action: "patch_transaction", Workdir: root, Changes: []ChangeRequest{existingTransactionChange(t, target, &updated)}}
	ops := defaultTransactionOps()
	original := ops.fsyncDirStrict
	failed := false
	ops.fsyncDirStrict = func(path string) error {
		if !failed && path == stateDir {
			failed = true
			return errors.New("injected journal directory fsync failure")
		}
		return original(path)
	}
	if _, err := dispatchWithOps(req, ops); err == nil {
		t.Fatal("expected journal directory fsync failure")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old\n" {
		t.Fatalf("target changed before durable journal: %q err=%v", got, err)
	}
	recovery, err := recoverPatchTransactions(&Request{Workdir: root}, defaultTransactionOps())
	if err != nil {
		t.Fatal(err)
	}
	if recovery.RecoveredTransactions == nil || *recovery.RecoveredTransactions != 1 {
		t.Fatalf("recovery response = %#v", recovery)
	}
}

func TestWSLPatchTransactionRollsBackWhenDataDirectoryFsyncFails(t *testing.T) {
	root := t.TempDir()
	withTransactionState(t)
	target := filepath.Join(root, "target.txt")
	mustWriteFile(t, target, "old\n", 0o600)
	updated := "new\n"
	req := &Request{Action: "patch_transaction", Workdir: root, Changes: []ChangeRequest{existingTransactionChange(t, target, &updated)}}
	ops := defaultTransactionOps()
	original := ops.fsyncDirStrict
	failed := false
	ops.fsyncDirStrict = func(path string) error {
		if !failed && path == root {
			failed = true
			return errors.New("injected data directory fsync failure")
		}
		return original(path)
	}
	if _, err := dispatchWithOps(req, ops); err == nil {
		t.Fatal("expected data directory fsync failure")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old\n" {
		t.Fatalf("target not restored after data fsync failure: %q err=%v", got, err)
	}
	assertNoTransactionArtifacts(t, root)
}

func TestWSLPatchTransactionCrashRecoveryRollsBackUncommitted(t *testing.T) {
	root := t.TempDir()
	state := withTransactionState(t)
	first := filepath.Join(root, "first.txt")
	second := filepath.Join(root, "second.txt")
	mustWriteFile(t, first, "first old\n", 0o600)
	mustWriteFile(t, second, "second old\n", 0o600)
	firstNew, secondNew := "first new\n", "second new\n"
	req := Request{Action: "patch_transaction", Workdir: root, Changes: []ChangeRequest{
		existingTransactionChange(t, first, &firstNew), existingTransactionChange(t, second, &secondNew),
	}}
	runCrashWorker(t, state, req, "after-first-install", 91)
	recovery, err := recoverPatchTransactions(&Request{Workdir: root}, defaultTransactionOps())
	if err != nil {
		t.Fatal(err)
	}
	if recovery.RecoveredTransactions == nil || *recovery.RecoveredTransactions != 1 {
		t.Fatalf("recovery response = %#v", recovery)
	}
	for path, want := range map[string]string{first: "first old\n", second: "second old\n"} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Fatalf("%s not restored after crash: %q err=%v", path, got, err)
		}
	}
}

func TestWSLPatchTransactionRecoversLinkFallbackCrashDuringBackup(t *testing.T) {
	root := t.TempDir()
	state := withTransactionState(t)
	path := filepath.Join(root, "file.txt")
	mustWriteFile(t, path, "old\n", 0o600)
	updated := "new\n"
	req := Request{Action: "patch_transaction", Workdir: root, Changes: []ChangeRequest{existingTransactionChange(t, path, &updated)}}
	runCrashWorker(t, state, req, "backup-link", 93)
	if _, err := recoverPatchTransactions(&Request{Workdir: root}, defaultTransactionOps()); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "old\n" {
		t.Fatalf("original file changed after backup-link crash: %q err=%v", got, err)
	}
	assertNoTransactionArtifacts(t, root)
}

func TestWSLPatchTransactionCrashAfterCommitKeepsCommittedFiles(t *testing.T) {
	root := t.TempDir()
	state := withTransactionState(t)
	path := filepath.Join(root, "file.txt")
	mustWriteFile(t, path, "old\n", 0o600)
	updated := "new\n"
	req := Request{Action: "patch_transaction", Workdir: root, Changes: []ChangeRequest{existingTransactionChange(t, path, &updated)}}
	runCrashWorker(t, state, req, "before-cleanup", 92)
	if _, err := recoverPatchTransactions(&Request{Workdir: root}, defaultTransactionOps()); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != updated {
		t.Fatalf("committed file rolled back after cleanup crash: %q err=%v", got, err)
	}
	assertNoTransactionArtifacts(t, root)
}

func TestWSLPatchTransactionCrashWorker(t *testing.T) {
	crashCase := os.Getenv("AGENTDOCK_WSL_HELPER_TEST_CRASH_CASE")
	if crashCase == "" {
		t.Skip("crash worker only runs in subprocess")
	}
	payload, err := base64.StdEncoding.DecodeString(os.Getenv("AGENTDOCK_WSL_HELPER_TEST_REQUEST"))
	if err != nil {
		t.Fatal(err)
	}
	var req Request
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("XDG_STATE_HOME", os.Getenv("AGENTDOCK_WSL_HELPER_TEST_STATE")); err != nil {
		t.Fatal(err)
	}
	ops := defaultTransactionOps()
	originalRename := ops.renameNoReplace
	switch crashCase {
	case "after-first-install":
		installs := 0
		ops.renameNoReplace = func(source, destination string) error {
			err := originalRename(source, destination)
			if err == nil && strings.HasPrefix(filepath.Base(source), ".agentdock-patch-write-") {
				installs++
				if installs == 1 {
					os.Exit(91)
				}
			}
			return err
		}
	case "backup-link":
		ops.renameNoReplace = func(source, destination string) error {
			if strings.HasPrefix(filepath.Base(destination), ".agentdock-patch-backup-") {
				if err := os.Link(source, destination); err != nil {
					return err
				}
				os.Exit(93)
			}
			return originalRename(source, destination)
		}
	case "before-cleanup":
		ops.cleanupCommitted = func(journal transactionJournal, ops transactionOps) []string {
			os.Exit(92)
			return nil
		}
	default:
		t.Fatalf("unknown crash case %q", crashCase)
	}
	if _, err := patchTransaction(&req, ops); err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash worker transaction returned without crashing")
}

func runCrashWorker(t *testing.T, state string, req Request, crashCase string, wantExit int) {
	t.Helper()
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWSLPatchTransactionCrashWorker$")
	cmd.Env = append(os.Environ(),
		"AGENTDOCK_WSL_HELPER_TEST_CRASH_CASE="+crashCase,
		"AGENTDOCK_WSL_HELPER_TEST_STATE="+state,
		"AGENTDOCK_WSL_HELPER_TEST_REQUEST="+base64.StdEncoding.EncodeToString(payload),
	)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("crash worker unexpectedly succeeded: %s", output)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != wantExit {
		t.Fatalf("crash worker exit=%v output=%s, want %d", err, output, wantExit)
	}
}

func assertNoTransactionArtifacts(t *testing.T, root string) {
	t.Helper()
	var artifacts []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".agentdock-patch-") {
			artifacts = append(artifacts, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("transaction artifacts remain: %#v", artifacts)
	}
}

func TestTransactionJournalDoesNotPersistContent(t *testing.T) {
	root := t.TempDir()
	withTransactionState(t)
	path := filepath.Join(root, "file.txt")
	mustWriteFile(t, path, "old\n", 0o600)
	updated := "secret-new-content\n"
	change := existingTransactionChange(t, path, &updated)
	transactionID := "testtx"
	items, _, err := normalizeTransactionChanges(&Request{Changes: []ChangeRequest{change}}, transactionID)
	if err != nil {
		t.Fatal(err)
	}
	journal := transactionJournal{Version: 1, TransactionID: transactionID, Workdir: root, Phase: "prepared", Items: items}
	data, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), updated) || strings.Contains(string(data), "content") {
		t.Fatalf("journal leaks patch content: %s", data)
	}
}

func TestRenameNoReplaceDoesNotOverwriteDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")
	mustWriteFile(t, source, "source", 0o600)
	mustWriteFile(t, destination, "destination", 0o600)
	if err := renameNoReplace(source, destination); !errors.Is(err, os.ErrExist) {
		t.Fatalf("renameNoReplace error=%v, want file exists", err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "destination" {
		t.Fatalf("destination overwritten: %q err=%v", got, err)
	}
}

func TestFailureEnvelopeKeepsTransactionErrorCode(t *testing.T) {
	err := fail("PATCH_CONFLICT", "conflict", map[string]any{"path": "/tmp/x"})
	failure := failureFrom(err)
	if failure.Code != "PATCH_CONFLICT" || failure.Message != "conflict" {
		t.Fatalf("failure = %#v", failure)
	}
	if fmt.Sprint(failure.Details["path"]) != "/tmp/x" {
		t.Fatalf("details = %#v", failure.Details)
	}
}
