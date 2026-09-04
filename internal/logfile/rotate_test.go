package logfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultRotationPolicy(t *testing.T) {
	if DefaultMaxBytes != 10<<20 {
		t.Fatalf("DefaultMaxBytes=%d, want %d", DefaultMaxBytes, int64(10<<20))
	}
	if DefaultMaxFiles != 5 {
		t.Fatalf("DefaultMaxFiles=%d, want 5", DefaultMaxFiles)
	}
}

func TestWriterRotatesAndBoundsFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentdock.err.log")
	writer, err := Open(path, 10, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("abcdefghijklmnopqrstuvwxyz")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"agentdock.err.log", "agentdock.err.log.1", "agentdock.err.log.2"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
		if info.Size() > 10 {
			t.Fatalf("%s size=%d, want <= 10", name, info.Size())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "agentdock.err.log.3")); !os.IsNotExist(err) {
		t.Fatalf("unexpected fourth retained file: %v", err)
	}

	active, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "uvwxyz" {
		t.Fatalf("active=%q, want %q", active, "uvwxyz")
	}
}

func TestWriterRotatesExistingFullFileOnOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentdock.err.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 10)), 0o644); err != nil {
		t.Fatal(err)
	}

	writer, err := Open(path, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != strings.Repeat("x", 10) {
		t.Fatalf("backup=%q", backup)
	}
	active, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != "new" {
		t.Fatalf("active=%q", active)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows 的 os.FileMode 不表达 ACL，只有 POSIX 平台才能用 mode 验证 0600。
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o, want 600", info.Mode().Perm())
	}
}

func TestWriterCapsOversizedLegacyFileBeforeRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentdock.err.log")
	legacy := "0123456789ABCDEFGHIJ"
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	writer, err := Open(path, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "ABCDEFGHIJ" {
		t.Fatalf("backup=%q, want newest 10 bytes", backup)
	}
	info, err := os.Stat(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 10 {
		t.Fatalf("legacy backup size=%d, want <= 10", info.Size())
	}
}
