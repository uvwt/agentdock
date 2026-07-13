//go:build linux

package tools

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func runWSLFileHelperForTest(t *testing.T, request map[string]any) Result {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for WSL file helper tests")
	}
	script, err := os.ReadFile("wsl_file_helper.py")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-c", string(script))
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run helper: %v\n%s", err, output)
	}
	result := Result{}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode helper result: %v\n%s", err, output)
	}
	return result
}

func requireWSLHelperOK(t *testing.T, result Result) {
	t.Helper()
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("helper result = %#v", result)
	}
}

func TestWSLFileHelperReadsListsAndSearchesNativeTree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"a.txt":            "first\nneedle here\nlast\n",
		"sub/b.go":         "package sample\n// Needle in Go\n",
		"unicode.txt":      "中文needle unicode\n",
		".hidden.txt":      "needle hidden\n",
		"ignored/skip.txt": "needle ignored\n",
		".gitignore":       "ignored/\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	read := runWSLFileHelperForTest(t, map[string]any{"action": "read", "path": filepath.Join(root, "a.txt")})
	requireWSLHelperOK(t, read)
	if read["content"] != files["a.txt"] || read["size_bytes"].(float64) != float64(len(files["a.txt"])) {
		t.Fatalf("read result = %#v", read)
	}

	listed := runWSLFileHelperForTest(t, map[string]any{
		"action": "list_dir", "path": root, "recursive": true, "max_depth": 3,
		"include_hidden": false, "include_ignored": false, "max_entries": 100,
	})
	requireWSLHelperOK(t, listed)
	entries := listed["entries"].([]any)
	for _, raw := range entries {
		entry := raw.(map[string]any)
		if entry["relative_path"] == ".hidden.txt" || entry["relative_path"] == "ignored" || entry["relative_path"] == "ignored/skip.txt" {
			t.Fatalf("hidden or ignored entry leaked: %#v", entry)
		}
	}

	filesResult := runWSLFileHelperForTest(t, map[string]any{
		"action": "list_files", "path": root, "patterns": []string{"**/*.go"},
		"include_hidden": false, "include_ignored": false, "max_results": 100,
	})
	requireWSLHelperOK(t, filesResult)
	matchedFiles := filesResult["files"].([]any)
	if len(matchedFiles) != 1 || matchedFiles[0].(map[string]any)["relative_path"] != "sub/b.go" {
		t.Fatalf("list_files result = %#v", filesResult)
	}

	searched := runWSLFileHelperForTest(t, map[string]any{
		"action": "search_text", "path": root, "query": "needle", "case_sensitive": false,
		"include_hidden": false, "include_ignored": false, "context_lines": 1, "max_results": 100,
	})
	requireWSLHelperOK(t, searched)
	matches := searched["matches"].([]any)
	if len(matches) != 3 {
		t.Fatalf("search result = %#v", searched)
	}
	for _, raw := range matches {
		match := raw.(map[string]any)
		if match["relative_path"] == "unicode.txt" && match["column"] != float64(7) {
			t.Fatalf("unicode byte column = %#v, want 7", match["column"])
		}
	}
}

func TestWSLFileHelperAtomicWritePreservesModeAndRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "deploy.sh")
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho old\n"), 0o750); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	beforeStat, ok := before.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("target stat does not expose ownership")
	}
	written := runWSLFileHelperForTest(t, map[string]any{
		"action": "write_atomic", "path": target, "content": "#!/bin/sh\necho new\n",
		"must_exist": true, "overwrite": true,
	})
	requireWSLHelperOK(t, written)
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("mode = %o, want 750", info.Mode().Perm())
	}
	afterStat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || afterStat.Uid != beforeStat.Uid || afterStat.Gid != beforeStat.Gid {
		t.Fatalf("ownership changed from %d:%d to %d:%d", beforeStat.Uid, beforeStat.Gid, afterStat.Uid, afterStat.Gid)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "#!/bin/sh\necho new\n" {
		t.Fatalf("content = %q", content)
	}
	if temporary, err := filepath.Glob(filepath.Join(root, ".agentdock-atomic-*")); err != nil || len(temporary) != 0 {
		t.Fatalf("temporary files = %#v, err = %v", temporary, err)
	}

	inherited := filepath.Join(root, "inherited.sh")
	inheritedWrite := runWSLFileHelperForTest(t, map[string]any{
		"action": "write_atomic", "path": inherited, "content": "#!/bin/sh\n",
		"mode": 0o740, "owner_uid": beforeStat.Uid, "owner_gid": beforeStat.Gid,
	})
	requireWSLHelperOK(t, inheritedWrite)
	inheritedInfo, err := os.Stat(inherited)
	if err != nil {
		t.Fatal(err)
	}
	inheritedStat := inheritedInfo.Sys().(*syscall.Stat_t)
	if inheritedInfo.Mode().Perm() != 0o740 || inheritedStat.Uid != beforeStat.Uid || inheritedStat.Gid != beforeStat.Gid {
		t.Fatalf("inherited mode/owner = %o %d:%d", inheritedInfo.Mode().Perm(), inheritedStat.Uid, inheritedStat.Gid)
	}

	link := filepath.Join(root, "link.sh")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	rejected := runWSLFileHelperForTest(t, map[string]any{
		"action": "write_atomic", "path": link, "content": "broken\n",
		"must_exist": true, "overwrite": true,
	})
	if rejected["ok"] != false || rejected["code"] != "SYMLINK_NOT_ALLOWED" {
		t.Fatalf("symlink write result = %#v", rejected)
	}
	content, err = os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "#!/bin/sh\necho new\n" {
		t.Fatalf("symlink rejection changed target: %q", content)
	}

	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	linkedDirectory := filepath.Join(root, "linked")
	if err := os.Symlink(realDirectory, linkedDirectory); err != nil {
		t.Fatal(err)
	}
	parentRejected := runWSLFileHelperForTest(t, map[string]any{
		"action": "write_atomic", "path": filepath.Join(linkedDirectory, "child.txt"), "content": "blocked\n",
	})
	if parentRejected["ok"] != false || parentRejected["code"] != "SYMLINK_NOT_ALLOWED" {
		t.Fatalf("symlink parent result = %#v", parentRejected)
	}
	if _, err := os.Stat(filepath.Join(realDirectory, "child.txt")); !os.IsNotExist(err) {
		t.Fatalf("symlink parent rejection created target, err=%v", err)
	}
}

func TestWSLFileHelperMovesDeletesAndBlocksProtectedPaths(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	destination := filepath.Join(root, "nested", "destination.txt")
	if err := os.WriteFile(source, []byte("move me\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	moved := runWSLFileHelperForTest(t, map[string]any{"action": "move", "path": source, "new_path": destination})
	requireWSLHelperOK(t, moved)
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
	if content, err := os.ReadFile(destination); err != nil || string(content) != "move me\n" {
		t.Fatalf("destination content = %q, err = %v", content, err)
	}
	deleted := runWSLFileHelperForTest(t, map[string]any{"action": "delete", "path": destination})
	requireWSLHelperOK(t, deleted)
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination still exists: %v", err)
	}

	protected := runWSLFileHelperForTest(t, map[string]any{
		"action": "write_atomic", "path": "/proc/agentdock-test", "content": "blocked\n", "overwrite": true,
	})
	if protected["ok"] != false || protected["code"] != "PROTECTED_WSL_PATH" {
		t.Fatalf("protected path result = %#v", protected)
	}
}
