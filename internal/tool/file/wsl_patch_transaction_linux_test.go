//go:build linux

package file

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func runWSLPatchTransactionHelper(t *testing.T, stateDir string, request map[string]any, wrapper string) (Result, error) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for WSL patch transaction tests")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	code := wrapper
	if code == "" {
		script, err := os.ReadFile("wsl_file_helper.py")
		if err != nil {
			t.Fatal(err)
		}
		code = string(script)
	}
	cmd := exec.Command(python, "-c", code)
	cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+stateDir)
	cmd.Stdin = bytes.NewReader(payload)
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return nil, fmt.Errorf("run helper: %w: %s", runErr, output)
	}
	result := Result{}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("decode helper result: %w: %s", err, output)
	}
	return result, nil
}

func requirePatchTransactionOK(t *testing.T, result Result, err error) Result {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("helper result = %#v", result)
	}
	return result
}

func existingPatchChange(t *testing.T, path string, content *string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	statInfo, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("file stat does not expose ownership")
	}
	sum := sha256.Sum256(data)
	change := map[string]any{
		"path":            path,
		"expected_exists": true,
		"expected_sha256": fmt.Sprintf("%x", sum),
		"expected_mode":   int(info.Mode().Perm()),
		"expected_uid":    int(statInfo.Uid),
		"expected_gid":    int(statInfo.Gid),
		"new_exists":      content != nil,
	}
	if content != nil {
		newSum := sha256.Sum256([]byte(*content))
		change["content"] = *content
		change["sha256"] = fmt.Sprintf("%x", newSum)
		change["mode"] = int(info.Mode().Perm())
		change["owner_uid"] = int(statInfo.Uid)
		change["owner_gid"] = int(statInfo.Gid)
	}
	return change
}

func newPatchChange(path, content string, mode int) map[string]any {
	sum := sha256.Sum256([]byte(content))
	return map[string]any{
		"path":            path,
		"expected_exists": false,
		"new_exists":      true,
		"content":         content,
		"sha256":          fmt.Sprintf("%x", sum),
		"mode":            mode,
	}
}

func pythonHelperWrapper(body string) string {
	return `
import importlib.util
import os
spec = importlib.util.spec_from_file_location("agentdock_wsl_file_helper", "wsl_file_helper.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
` + body + `
module.main()
`
}

func TestWSLPatchTransactionCommitsMultipleFiles(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	updatePath := filepath.Join(root, "update.txt")
	deletePath := filepath.Join(root, "delete.txt")
	moveSource := filepath.Join(root, "move-source.txt")
	moveDestination := filepath.Join(root, "nested", "move-destination.txt")
	addPath := filepath.Join(root, "nested", "add.txt")

	for path, content := range map[string]string{
		updatePath: "old update\n",
		deletePath: "delete me\n",
		moveSource: "move me\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	updated := "new update\n"
	moved := "move me\n"
	changes := []map[string]any{
		existingPatchChange(t, updatePath, &updated),
		existingPatchChange(t, deletePath, nil),
		existingPatchChange(t, moveSource, nil),
		newPatchChange(moveDestination, moved, 0o640),
		newPatchChange(addPath, "added\n", 0o644),
	}
	result, err := runWSLPatchTransactionHelper(t, stateDir, map[string]any{
		"action": "patch_transaction", "workdir": root, "changes": changes,
	}, "")
	requirePatchTransactionOK(t, result, err)

	for path, want := range map[string]string{
		updatePath:      updated,
		moveDestination: moved,
		addPath:         "added\n",
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s content = %q, err=%v, want %q", path, got, err, want)
		}
	}
	for _, path := range []string{deletePath, moveSource} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after transaction: %v", path, err)
		}
	}
	artifacts := make([]string, 0)
	if err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".agentdock-patch-") {
			artifacts = append(artifacts, current)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("transaction artifacts remain: %#v", artifacts)
	}
}

func TestWSLPatchTransactionConflictAbortsBeforeCommit(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	first := filepath.Join(root, "first.txt")
	second := filepath.Join(root, "second.txt")
	if err := os.WriteFile(first, []byte("first old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstNew := "first new\n"
	secondNew := "second new\n"
	firstChange := existingPatchChange(t, first, &firstNew)
	secondChange := existingPatchChange(t, second, &secondNew)
	secondChange["expected_sha256"] = string(make([]byte, 64))

	result, err := runWSLPatchTransactionHelper(t, stateDir, map[string]any{
		"action": "patch_transaction", "workdir": root, "changes": []map[string]any{firstChange, secondChange},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result["ok"] != false || result["code"] != "PATCH_CONFLICT" {
		t.Fatalf("conflict result = %#v", result)
	}
	for path, want := range map[string]string{first: "first old\n", second: "second old\n"} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || string(got) != want {
			t.Fatalf("%s changed despite conflict: %q, err=%v", path, got, readErr)
		}
	}
}

func TestWSLPatchTransactionRollsBackPartialInstall(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	first := filepath.Join(root, "first.txt")
	second := filepath.Join(root, "second.txt")
	if err := os.WriteFile(first, []byte("first old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstNew := "first new\n"
	secondNew := "second new\n"
	request := map[string]any{
		"action": "patch_transaction", "workdir": root,
		"changes": []map[string]any{existingPatchChange(t, first, &firstNew), existingPatchChange(t, second, &secondNew)},
	}
	wrapper := pythonHelperWrapper(`
original = module.rename_no_replace
install_count = 0
def fail_second_install(source, destination):
    global install_count
    if os.path.basename(source).startswith(".agentdock-patch-write-"):
        install_count += 1
        if install_count == 2:
            raise OSError(5, "injected install failure")
    return original(source, destination)
module.rename_no_replace = fail_second_install
`)
	result, err := runWSLPatchTransactionHelper(t, stateDir, request, wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if result["ok"] != false {
		t.Fatalf("injected failure unexpectedly succeeded: %#v", result)
	}
	for path, want := range map[string]string{first: "first old\n", second: "second old\n"} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || string(got) != want {
			t.Fatalf("%s was not rolled back: %q, err=%v", path, got, readErr)
		}
	}
}

func TestWSLPatchTransactionRestoresExternalChangeDetectedDuringBackup(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := "transaction\n"
	request := map[string]any{
		"action": "patch_transaction", "workdir": root,
		"changes": []map[string]any{existingPatchChange(t, target, &updated)},
	}
	wrapper := pythonHelperWrapper(`
original = module.rename_no_replace
def mutate_before_backup(source, destination):
    if os.path.basename(destination).startswith(".agentdock-patch-backup-"):
        with open(source, "w", encoding="utf-8") as handle:
            handle.write("external\n")
    return original(source, destination)
module.rename_no_replace = mutate_before_backup
`)
	result, err := runWSLPatchTransactionHelper(t, stateDir, request, wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if result["ok"] != false || result["code"] != "PATCH_CONFLICT" {
		t.Fatalf("backup conflict result = %#v", result)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "external\n" {
		t.Fatalf("external change was not restored to original path: %q, err=%v", got, err)
	}
	backups, err := filepath.Glob(filepath.Join(root, ".agentdock-patch-backup-*"))
	if err != nil || len(backups) != 0 {
		t.Fatalf("backup artifacts remain after conflict rollback: %#v, err=%v", backups, err)
	}
}

func TestWSLPatchTransactionPreservesConcurrentTarget(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	existing := filepath.Join(root, "existing.txt")
	concurrent := filepath.Join(root, "concurrent.txt")
	if err := os.WriteFile(existing, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := "new\n"
	request := map[string]any{
		"action": "patch_transaction", "workdir": root,
		"changes": []map[string]any{existingPatchChange(t, existing, &updated), newPatchChange(concurrent, "transaction\n", 0o644)},
	}
	wrapper := pythonHelperWrapper(`
original = module.rename_no_replace
def create_concurrent_target(source, destination):
    if os.path.basename(source).startswith(".agentdock-patch-write-") and destination.endswith("concurrent.txt"):
        with open(destination, "x", encoding="utf-8") as handle:
            handle.write("external\n")
    return original(source, destination)
module.rename_no_replace = create_concurrent_target
`)
	result, err := runWSLPatchTransactionHelper(t, stateDir, request, wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if result["ok"] != false || result["code"] != "PATCH_ROLLBACK_INCOMPLETE" {
		t.Fatalf("concurrent target result = %#v", result)
	}
	got, err := os.ReadFile(existing)
	if err != nil || string(got) != "old\n" {
		t.Fatalf("existing target not restored: %q, err=%v", got, err)
	}
	got, err = os.ReadFile(concurrent)
	if err != nil || string(got) != "external\n" {
		t.Fatalf("concurrent target was overwritten: %q, err=%v", got, err)
	}
}

func TestWSLPatchTransactionCrashRecoveryRollsBackUncommitted(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	first := filepath.Join(root, "first.txt")
	second := filepath.Join(root, "second.txt")
	if err := os.WriteFile(first, []byte("first old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstNew := "first new\n"
	secondNew := "second new\n"
	request := map[string]any{
		"action": "patch_transaction", "workdir": root,
		"changes": []map[string]any{existingPatchChange(t, first, &firstNew), existingPatchChange(t, second, &secondNew)},
	}
	wrapper := pythonHelperWrapper(`
original = module.rename_no_replace
install_count = 0
def crash_after_first_install(source, destination):
    global install_count
    result = original(source, destination)
    if os.path.basename(source).startswith(".agentdock-patch-write-"):
        install_count += 1
        if install_count == 1:
            os._exit(91)
    return result
module.rename_no_replace = crash_after_first_install
`)
	if _, err := runWSLPatchTransactionHelper(t, stateDir, request, wrapper); err == nil {
		t.Fatal("expected helper process to crash")
	}

	recovery, err := runWSLPatchTransactionHelper(t, stateDir, map[string]any{
		"action": "recover_patch_transactions", "workdir": root,
	}, "")
	requirePatchTransactionOK(t, recovery, err)
	if recovery["recovered_transactions"].(float64) != 1 {
		t.Fatalf("recovery result = %#v", recovery)
	}
	for path, want := range map[string]string{first: "first old\n", second: "second old\n"} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || string(got) != want {
			t.Fatalf("%s not restored after crash: %q, err=%v", path, got, readErr)
		}
	}
}

func TestWSLPatchTransactionRecoversJournalAfterDirectoryFsyncFailure(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := "new\n"
	request := map[string]any{
		"action": "patch_transaction", "workdir": root,
		"changes": []map[string]any{existingPatchChange(t, target, &updated)},
	}
	wrapper := pythonHelperWrapper(`
original = module.fsync_directory_strict
state_root = os.path.join(os.environ["XDG_STATE_HOME"], "agentdock", "wsl-patch-transactions")
failed = False
def fail_journal_directory_fsync(path):
    global failed
    if not failed and path.startswith(state_root + os.sep):
        failed = True
        raise OSError(5, "injected journal directory fsync failure")
    return original(path)
module.fsync_directory_strict = fail_journal_directory_fsync
`)
	result, err := runWSLPatchTransactionHelper(t, stateDir, request, wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if result["ok"] != false || result["code"] != "WSL_FILE_RUNTIME_ERROR" {
		t.Fatalf("journal fsync failure result = %#v", result)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old\n" {
		t.Fatalf("target changed before durable journal: %q, err=%v", got, err)
	}

	recovery, err := runWSLPatchTransactionHelper(t, stateDir, map[string]any{
		"action": "recover_patch_transactions", "workdir": root,
	}, "")
	requirePatchTransactionOK(t, recovery, err)
	if recovery["recovered_transactions"].(float64) != 1 {
		t.Fatalf("recovery result = %#v", recovery)
	}
}

func TestWSLPatchTransactionRollsBackWhenDataDirectoryFsyncFails(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := "new\n"
	request := map[string]any{
		"action": "patch_transaction", "workdir": root,
		"changes": []map[string]any{existingPatchChange(t, target, &updated)},
	}
	wrapper := pythonHelperWrapper(fmt.Sprintf(`
original = module.fsync_directory_strict
data_dir = %q
failed = False
def fail_first_data_directory_fsync(path):
    global failed
    if not failed and path == data_dir:
        failed = True
        raise OSError(5, "injected data directory fsync failure")
    return original(path)
module.fsync_directory_strict = fail_first_data_directory_fsync
`, root))
	result, err := runWSLPatchTransactionHelper(t, stateDir, request, wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if result["ok"] != false || result["code"] != "WSL_FILE_RUNTIME_ERROR" {
		t.Fatalf("data directory fsync failure result = %#v", result)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old\n" {
		t.Fatalf("target not restored after data directory fsync failure: %q, err=%v", got, err)
	}
	backups, err := filepath.Glob(filepath.Join(root, ".agentdock-patch-backup-*"))
	if err != nil || len(backups) != 0 {
		t.Fatalf("backup artifacts remain after fsync rollback: %#v, err=%v", backups, err)
	}
}

func TestWSLPatchTransactionRecoversLinkFallbackCrashDuringBackup(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := "new\n"
	request := map[string]any{
		"action": "patch_transaction", "workdir": root,
		"changes": []map[string]any{existingPatchChange(t, path, &updated)},
	}
	wrapper := pythonHelperWrapper(`
original = module.rename_no_replace
def crash_after_backup_link(source, destination):
    if os.path.basename(destination).startswith(".agentdock-patch-backup-"):
        os.link(source, destination)
        os._exit(93)
    return original(source, destination)
module.rename_no_replace = crash_after_backup_link
`)
	if _, err := runWSLPatchTransactionHelper(t, stateDir, request, wrapper); err == nil {
		t.Fatal("expected helper process to crash")
	}

	recovery, err := runWSLPatchTransactionHelper(t, stateDir, map[string]any{
		"action": "recover_patch_transactions", "workdir": root,
	}, "")
	requirePatchTransactionOK(t, recovery, err)
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "old\n" {
		t.Fatalf("original file changed after backup-link crash: %q, err=%v", got, err)
	}
	backups, err := filepath.Glob(filepath.Join(root, ".agentdock-patch-backup-*"))
	if err != nil || len(backups) != 0 {
		t.Fatalf("duplicate backups remain after recovery: %#v, err=%v", backups, err)
	}
}

func TestWSLPatchTransactionCrashAfterCommitKeepsCommittedFiles(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := "new\n"
	request := map[string]any{
		"action": "patch_transaction", "workdir": root,
		"changes": []map[string]any{existingPatchChange(t, path, &updated)},
	}
	wrapper := pythonHelperWrapper(`
def crash_before_cleanup(journal):
    os._exit(92)
module.cleanup_committed_transaction = crash_before_cleanup
`)
	if _, err := runWSLPatchTransactionHelper(t, stateDir, request, wrapper); err == nil {
		t.Fatal("expected helper process to crash")
	}

	recovery, err := runWSLPatchTransactionHelper(t, stateDir, map[string]any{
		"action": "recover_patch_transactions", "workdir": root,
	}, "")
	requirePatchTransactionOK(t, recovery, err)
	got, err := os.ReadFile(path)
	if err != nil || string(got) != updated {
		t.Fatalf("committed file was rolled back after cleanup crash: %q, err=%v", got, err)
	}
}
