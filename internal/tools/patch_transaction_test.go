package tools

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnvelopePatchAbsolutePathWritesTheResolvedTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix absolute path regression")
	}
	rt, workspaceRoot := newCodeToolsRuntime(t)
	externalRoot := t.TempDir()
	target := filepath.Join(externalRoot, "target.txt")
	if err := os.WriteFile(target, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: " + target + "\n@@\n-before\n+after\n*** End Patch"
	if _, err := rt.applyPatch(t.Context(), map[string]any{"patch": patch}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "after\n" {
		t.Fatalf("absolute target content = %q, want after", content)
	}
	mirror := filepath.Join(workspaceRoot, strings.TrimPrefix(filepath.Clean(target), string(filepath.Separator)))
	if _, err := os.Stat(mirror); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("patch created a workspace mirror at %s: %v", mirror, err)
	}
}

func TestEnvelopePatchMovePreservesExecutableMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows permissions use DACL rather than Unix mode bits")
	}
	rt, root := newCodeToolsRuntime(t)
	source := filepath.Join(root, "source.sh")
	if err := os.WriteFile(source, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: source.sh\n*** Move to: moved.sh\n@@\n-echo old\n+echo new\n*** End Patch"
	if _, err := rt.applyPatch(t.Context(), map[string]any{"patch": patch}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source still exists after move: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, "moved.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("moved file mode = %04o, want 0755", got)
	}
}

func TestCommitStagedPatchRollsBackEarlierFilesWhenInstallFails(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "a.txt")
	second := filepath.Join(root, "b.txt")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	updated := "new\n"
	staged := map[string]stagedPatchFile{
		first:  {Abs: first, Display: "a.txt", Content: &updated, Mode: 0o600, Original: []byte("old\n"), OriginalExists: true},
		second: {Abs: second, Display: "b.txt", Content: &updated, Mode: 0o600, Original: []byte("old\n"), OriginalExists: true},
	}
	installFailure := errors.New("simulated second install failure")
	failed := false
	link := func(source, target string) error {
		if !failed && target == second {
			failed = true
			return installFailure
		}
		return os.Link(source, target)
	}
	err := commitStagedPatchWithFileOps(staged, os.Rename, link)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "PATCH_CONFLICT" {
		t.Fatalf("commit error = %#v, want PATCH_CONFLICT", err)
	}
	for _, path := range []string{first, second} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read restored %s: %v", path, err)
		}
		if string(content) != "old\n" {
			t.Fatalf("restored %s content = %q, want old", path, content)
		}
	}
	leftovers, err := filepath.Glob(filepath.Join(root, ".agentdock-patch-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("patch transaction leftovers = %v", leftovers)
	}
}

func TestCommitStagedPatchRestoresBackupWhenInstalledFileIsDeletedDuringRollback(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "a.txt")
	second := filepath.Join(root, "b.txt")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	updated := "new\n"
	staged := map[string]stagedPatchFile{
		first:  {Abs: first, Display: "a.txt", Content: &updated, Mode: 0o600, Original: []byte("old\n"), OriginalExists: true},
		second: {Abs: second, Display: "b.txt", Content: &updated, Mode: 0o600, Original: []byte("old\n"), OriginalExists: true},
	}
	installFailure := errors.New("simulated second install failure")
	link := func(source, target string) error {
		if target == second {
			if err := os.Remove(first); err != nil {
				return err
			}
			return installFailure
		}
		return os.Link(source, target)
	}
	err := commitStagedPatchWithFileOps(staged, os.Rename, link)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "PATCH_CONFLICT" {
		t.Fatalf("commit error = %#v, want PATCH_CONFLICT", err)
	}
	for _, path := range []string{first, second} {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read restored %s: %v", path, readErr)
		}
		if string(content) != "old\n" {
			t.Fatalf("restored %s content = %q, want old", path, content)
		}
	}
	leftovers, globErr := filepath.Glob(filepath.Join(root, ".agentdock-patch-*"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(leftovers) != 0 {
		t.Fatalf("patch transaction leftovers = %v", leftovers)
	}
}

func TestCommitStagedPatchPreservesConcurrentChangeBeforeBackup(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := "patched\n"
	staged := map[string]stagedPatchFile{
		path: {Abs: path, Display: "target.txt", Content: &updated, Mode: 0o600, Original: []byte("old\n"), OriginalExists: true},
	}
	changed := false
	rename := func(source, target string) error {
		if !changed && source == path && strings.Contains(filepath.Base(target), ".agentdock-patch-backup-") {
			changed = true
			if err := os.WriteFile(path, []byte("concurrent\n"), 0o600); err != nil {
				return err
			}
		}
		return os.Rename(source, target)
	}
	err := commitStagedPatchWithRename(staged, rename)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "PATCH_CONFLICT" {
		t.Fatalf("commit error = %#v, want PATCH_CONFLICT", err)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "concurrent\n" {
		t.Fatalf("restored content = %q, want concurrent change", content)
	}
}

func TestCommitStagedPatchDoesNotOverwriteConcurrentlyCreatedTarget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "new.txt")
	updated := "patched\n"
	staged := map[string]stagedPatchFile{
		path: {Abs: path, Display: "new.txt", Content: &updated, Mode: 0o644},
	}
	link := func(source, target string) error {
		if err := os.WriteFile(target, []byte("concurrent\n"), 0o600); err != nil {
			return err
		}
		return os.Link(source, target)
	}
	err := commitStagedPatchWithFileOps(staged, os.Rename, link)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "PATCH_CONFLICT" {
		t.Fatalf("commit error = %#v, want PATCH_CONFLICT", err)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "concurrent\n" {
		t.Fatalf("target content = %q, want concurrent content", content)
	}
	leftovers, globErr := filepath.Glob(filepath.Join(root, ".agentdock-patch-*"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(leftovers) != 0 {
		t.Fatalf("patch transaction leftovers = %v", leftovers)
	}
}

func TestCommitStagedPatchDoesNotOverwriteTargetRecreatedAfterBackup(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := "patched\n"
	staged := map[string]stagedPatchFile{
		path: {Abs: path, Display: "target.txt", Content: &updated, Mode: 0o600, Original: []byte("old\n"), OriginalExists: true},
	}
	recreated := false
	rename := func(source, target string) error {
		if source == path && strings.Contains(filepath.Base(target), ".agentdock-patch-backup-") {
			if err := os.Rename(source, target); err != nil {
				return err
			}
			recreated = true
			return os.WriteFile(path, []byte("concurrent\n"), 0o600)
		}
		return os.Rename(source, target)
	}
	err := commitStagedPatchWithFileOps(staged, rename, os.Link)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "PATCH_CONFLICT" {
		t.Fatalf("commit error = %#v, want PATCH_CONFLICT", err)
	}
	if !recreated {
		t.Fatal("test did not recreate target after backup")
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "concurrent\n" {
		t.Fatalf("target content = %q, want concurrent content", content)
	}
}

func TestEnvelopePatchRejectsDuplicateNewTarget(t *testing.T) {
	rt, _ := newCodeToolsRuntime(t)
	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Add File: duplicate.txt",
		"+first",
		"*** Add File: duplicate.txt",
		"+second",
		"*** End Patch",
	}, "\n")
	if _, err := rt.applyPatch(t.Context(), map[string]any{"patch": patch, "dry_run": true}); err == nil {
		t.Fatal("duplicate add target was accepted")
	}
}

func TestEnvelopePatchRejectsNonTextTargets(t *testing.T) {
	cases := []struct {
		name    string
		content []byte
		code    string
	}{
		{name: "binary", content: []byte("prefix\x00suffix\n"), code: "BINARY_FILE"},
		{name: "invalid utf8", content: []byte{0xff, 0xfe}, code: "ENCODING_UNSUPPORTED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt, root := newCodeToolsRuntime(t)
			path := filepath.Join(root, "target.txt")
			if err := os.WriteFile(path, tc.content, 0o600); err != nil {
				t.Fatal(err)
			}
			patch := "*** Begin Patch\n*** Update File: target.txt\n@@\n-missing\n+value\n*** End Patch"
			_, err := rt.applyPatch(t.Context(), map[string]any{"patch": patch, "dry_run": true})
			var toolErr *ToolError
			if !errors.As(err, &toolErr) || toolErr.Code != tc.code {
				t.Fatalf("applyPatch() error = %#v, want %s", err, tc.code)
			}
		})
	}
}

func TestEnvelopePatchRejectsOversizedTarget(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	path := filepath.Join(root, "target.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxTextFileReadBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: target.txt\n@@\n-missing\n+value\n*** End Patch"
	_, err = rt.applyPatch(t.Context(), map[string]any{"patch": patch, "dry_run": true})
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "FILE_TOO_LARGE" {
		t.Fatalf("applyPatch() error = %#v, want FILE_TOO_LARGE", err)
	}
}
