package skillruntime

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDigestFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "package.zip")
	content := []byte("agentdock-package")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := DigestFile(path)
	if err != nil {
		t.Fatalf("DigestFile() error = %v", err)
	}
	sum := sha256.Sum256(content)
	want := "sha256:" + hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("DigestFile() = %q, want %q", got, want)
	}
}

func TestDigestDirectoryIsStableAndContentSensitive(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeDigestTree(t, first, []string{"b.txt", "nested/a.txt"})
	writeDigestTree(t, second, []string{"nested/a.txt", "b.txt"})

	firstDigest, err := DigestDirectory(first)
	if err != nil {
		t.Fatalf("DigestDirectory(first) error = %v", err)
	}
	secondDigest, err := DigestDirectory(second)
	if err != nil {
		t.Fatalf("DigestDirectory(second) error = %v", err)
	}
	if firstDigest != secondDigest {
		t.Fatalf("same tree digests differ: %s != %s", firstDigest, secondDigest)
	}
	if err := os.WriteFile(filepath.Join(second, "b.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	changedDigest, err := DigestDirectory(second)
	if err != nil {
		t.Fatalf("DigestDirectory(changed) error = %v", err)
	}
	if changedDigest == firstDigest {
		t.Fatal("digest did not change after file content changed")
	}
}

func TestDigestDirectoryRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "target"), []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := DigestDirectory(root); err == nil || !strings.Contains(err.Error(), "symlink is not allowed") {
		t.Fatalf("DigestDirectory() error = %v, want symlink rejection", err)
	}
}

func TestExtractZip(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "package.zip")
	writeZip(t, archive, []zipEntry{
		{name: "agentdock.yaml", body: "apiVersion: agentdock.dev/v1\n"},
		{name: "bin/run.sh", body: "#!/bin/sh\necho ok\n", mode: 0o755},
	})
	destination := t.TempDir()
	if err := extractZip(archive, destination, 1<<20); err != nil {
		t.Fatalf("extractZip() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "bin", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "#!/bin/sh\necho ok\n" {
		t.Fatalf("extracted content = %q", content)
	}
	info, err := os.Stat(filepath.Join(destination, "bin", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("executable bit was not preserved: %o", info.Mode().Perm())
	}
}

func TestExtractZipRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name      string
		entry     zipEntry
		maxBytes  int64
		wantError string
	}{
		{name: "parent traversal", entry: zipEntry{name: "../escape", body: "x"}, maxBytes: 100, wantError: "escapes package root"},
		{name: "nested traversal", entry: zipEntry{name: "a/../../escape", body: "x"}, maxBytes: 100, wantError: "escapes package root"},
		{name: "absolute path", entry: zipEntry{name: "/escape", body: "x"}, maxBytes: 100, wantError: "escapes package root"},
		{name: "symlink", entry: zipEntry{name: "link", body: "target", mode: os.ModeSymlink | 0o777}, maxBytes: 100, wantError: "symlink is not allowed"},
		{name: "size limit", entry: zipEntry{name: "large", body: "123456"}, maxBytes: 5, wantError: "exceeds 5 bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "package.zip")
			writeZip(t, archive, []zipEntry{test.entry})
			err := extractZip(archive, t.TempDir(), test.maxBytes)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("extractZip() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestExtractZipRejectsDuplicatePath(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "package.zip")
	writeZip(t, archive, []zipEntry{{name: "same.txt", body: "first"}, {name: "same.txt", body: "second"}})
	if err := extractZip(archive, t.TempDir(), 100); err == nil {
		t.Fatal("extractZip() accepted duplicate path")
	}
}

func TestNormalizeDigest(t *testing.T) {
	if got := normalizeDigest(" ABCDEF "); got != "sha256:abcdef" {
		t.Fatalf("normalizeDigest() = %q", got)
	}
	if got := normalizeDigest("SHA256:ABCDEF"); got != "sha256:abcdef" {
		t.Fatalf("normalizeDigest(prefixed) = %q", got)
	}
	if got := normalizeDigest("  "); got != "" {
		t.Fatalf("normalizeDigest(empty) = %q", got)
	}
}

type zipEntry struct {
	name string
	body string
	mode os.FileMode
}

func writeZip(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		mode := entry.mode
		if mode == 0 {
			mode = 0o600
		}
		header.SetMode(mode)
		part, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeDigestTree(t *testing.T, root string, paths []string) {
	t.Helper()
	for _, relative := range paths {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(relative), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
