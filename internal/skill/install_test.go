package skill

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	skillstate "github.com/uvwt/agentdock/internal/skill/state"
)

func TestInstallVersionlessSkillAsSingleCurrentContent(t *testing.T) {
	manager := newManagerForTest(t)
	source := writeSkillSource(t, "demo-skill", "First", nil)

	result, err := manager.Install(context.Background(), InstallRequest{Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if result.Skill != "demo-skill" || !result.Changed || result.ContentDigest == "" {
		t.Fatalf("unexpected install result: %#v", result)
	}
	wantPath := filepath.Join(manager.State.Root(), "demo-skill")
	if result.Path != wantPath {
		t.Fatalf("install path = %q, want %q", result.Path, wantPath)
	}
	data, err := os.ReadFile(filepath.Join(result.Path, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# First") {
		t.Fatalf("installed content = %s", data)
	}
	if _, err := os.Stat(filepath.Join(result.Path, ".agentdock-install.json")); !os.IsNotExist(err) {
		t.Fatalf("managed Skill must not carry install-state metadata: %v", err)
	}
}

func TestInstallSameContentIsNoOpAcrossDirectoryAndZip(t *testing.T) {
	manager := newManagerForTest(t)
	source := writeSkillSource(t, "demo-skill", "Same", map[string]string{"scripts/run.py": "print('ok')\n"})

	first, err := manager.Install(context.Background(), InstallRequest{Source: source})
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "demo-skill.zip")
	writeSkillArchive(t, source, archive)
	second, err := manager.Install(context.Background(), InstallRequest{Source: archive})
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Fatalf("same content unexpectedly changed current Skill: %#v", second)
	}
	if second.Path != first.Path || second.ContentDigest != first.ContentDigest {
		t.Fatalf("same content identity drifted: first=%#v second=%#v", first, second)
	}
}

func TestInstallChangedContentReplacesCurrentSkill(t *testing.T) {
	manager := newManagerForTest(t)
	firstSource := writeSkillSource(t, "demo-skill", "First", map[string]string{"references/old.md": "old\n"})
	first, err := manager.Install(context.Background(), InstallRequest{Source: firstSource})
	if err != nil {
		t.Fatal(err)
	}

	secondSource := writeSkillSource(t, "demo-skill", "Second", map[string]string{"references/new.md": "new\n"})
	second, err := manager.Install(context.Background(), InstallRequest{Source: secondSource})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Changed || second.ContentDigest == first.ContentDigest {
		t.Fatalf("changed content was not replaced: first=%#v second=%#v", first, second)
	}
	if _, err := os.Stat(filepath.Join(second.Path, "references", "old.md")); !os.IsNotExist(err) {
		t.Fatalf("old package content remained after replacement: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(second.Path, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# Second") {
		t.Fatalf("replacement content = %s", data)
	}
}

func TestInstallInvalidCandidateLeavesCurrentContentUnchanged(t *testing.T) {
	manager := newManagerForTest(t)
	current := writeSkillSource(t, "demo-skill", "Current", nil)
	first, err := manager.Install(context.Background(), InstallRequest{Source: current})
	if err != nil {
		t.Fatal(err)
	}

	invalid := writeSkillSource(t, "demo-skill", "Invalid", map[string]string{".env": "SECRET=value\n"})
	if _, err := manager.Install(context.Background(), InstallRequest{Source: invalid}); err == nil {
		t.Fatal("invalid update succeeded")
	}
	digest, err := DigestPackageContent(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	if digest != first.ContentDigest {
		t.Fatalf("current Skill changed after failed update: got %s want %s", digest, first.ContentDigest)
	}
	data, err := os.ReadFile(filepath.Join(first.Path, "SKILL.md"))
	if err != nil || !strings.Contains(string(data), "# Current") {
		t.Fatalf("current content lost after failed update: data=%s err=%v", data, err)
	}
}

func TestValidateRejectsLegacyManifestAndPrivateEnv(t *testing.T) {
	manager := newManagerForTest(t)
	for name, extra := range map[string]map[string]string{
		"legacy manifest": {"agentdock.yaml": "legacy\n"},
		"private env":     {".env": "SECRET=value\n"},
		"legacy receipt":  {".agentdock-install.json": "{}\n"},
	} {
		t.Run(name, func(t *testing.T) {
			source := writeSkillSource(t, "demo-skill", "Demo", extra)
			result, err := manager.Validate(context.Background(), ValidateRequest{Source: source})
			if err != nil {
				t.Fatal(err)
			}
			if result.Valid || len(result.Issues) == 0 {
				t.Fatalf("invalid package passed validation: %#v", result)
			}
		})
	}
}

func TestInstallRejectsFIFOWithoutTryingToHashIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not provide POSIX FIFO semantics for this regression test")
	}
	mkfifo, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skip("mkfifo is unavailable on this platform")
	}
	manager := newManagerForTest(t)
	source := writeSkillSource(t, "demo-skill", "Demo", nil)
	pipe := filepath.Join(source, "blocking-pipe")
	if output, err := exec.Command(mkfifo, pipe).CombinedOutput(); err != nil {
		t.Skipf("cannot create FIFO: %v: %s", err, output)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := manager.Install(ctx, InstallRequest{Source: source}); err == nil {
		t.Fatal("FIFO package unexpectedly installed")
	} else if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("installer blocked while hashing FIFO instead of rejecting the special file")
	}
}

func TestExtractZipRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "malicious.zip")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	entry, err := writer.Create("../escape.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(entry, "escaped"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "extracted")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := extractZip(archivePath, destination, 1<<20, 10000); err == nil || !strings.Contains(err.Error(), "escapes package root") {
		t.Fatalf("path traversal ZIP should be rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("path traversal wrote outside extraction root: %v", err)
	}
}

func newManagerForTest(t *testing.T) *Manager {
	t.Helper()
	state, err := skillstate.New(filepath.Join(t.TempDir(), "skills"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(state)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func writeSkillSource(t *testing.T, name, heading string, extra map[string]string) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	document := "---\nname: " + name + "\ndescription: Test Skill.\n---\n\n# " + heading + "\n"
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	for relative, content := range extra {
		target := filepath.Join(source, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return source
}

func writeSkillArchive(t *testing.T, source, archivePath string) {
	t.Helper()
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	walkErr := filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source || info.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		header.Method = zip.Deflate
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(entry, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if walkErr != nil {
		_ = writer.Close()
		_ = archive.Close()
		t.Fatal(walkErr)
	}
	if err := writer.Close(); err != nil {
		_ = archive.Close()
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareLocalSourceUsesPrivateSnapshot(t *testing.T) {
	manager := newManagerForTest(t)
	source := writeSkillSource(t, "snapshot-skill", "Before", map[string]string{"notes.txt": "before\n"})
	work, err := manager.State.TempPath("snapshot-proof")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(work)

	candidate, sourceDigest, err := manager.prepareSource(context.Background(), source, work, manager.MaxDownload, manager.MaxFiles)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(candidate) == filepath.Clean(source) {
		t.Fatal("local source was returned directly instead of being snapshotted")
	}
	before, err := os.ReadFile(filepath.Join(candidate, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "notes.txt"), []byte("mutated-after-snapshot\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(candidate, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("private snapshot changed with source: before=%q after=%q", before, after)
	}
	digest, err := DigestDirectory(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if digest != sourceDigest {
		t.Fatalf("snapshot digest drifted: got %s want %s", digest, sourceDigest)
	}
}

func TestLocalAndArchiveInstallEnforceFileAndByteLimits(t *testing.T) {
	t.Run("local-files", func(t *testing.T) {
		manager := newManagerForTest(t)
		source := writeSkillSource(t, "limited-skill", "Demo", map[string]string{"extra.txt": "x"})
		_, err := manager.Install(context.Background(), InstallRequest{Source: source, MaxFiles: 1})
		if err == nil || !strings.Contains(err.Error(), "exceeds 1 files") {
			t.Fatalf("local MaxFiles error = %v", err)
		}
	})

	t.Run("local-bytes", func(t *testing.T) {
		manager := newManagerForTest(t)
		source := writeSkillSource(t, "limited-skill", "Demo", map[string]string{"large.bin": strings.Repeat("x", 4096)})
		_, err := manager.Install(context.Background(), InstallRequest{Source: source, MaxBytes: 512, MaxFiles: 100})
		if err == nil || !strings.Contains(err.Error(), "byte") {
			t.Fatalf("local MaxBytes error = %v", err)
		}
	})

	t.Run("zip-files", func(t *testing.T) {
		manager := newManagerForTest(t)
		source := writeSkillSource(t, "limited-skill", "Demo", map[string]string{"extra.txt": "x"})
		archive := filepath.Join(t.TempDir(), "skill.zip")
		writeSkillArchive(t, source, archive)
		_, err := manager.Install(context.Background(), InstallRequest{Source: archive, MaxFiles: 1})
		if err == nil || !strings.Contains(err.Error(), "exceeds 1 files") {
			t.Fatalf("ZIP MaxFiles error = %v", err)
		}
	})
}
