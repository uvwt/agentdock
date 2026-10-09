package resourcelibrary

import (
	"archive/zip"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestExportDirectory_拒绝私人文件并保留可移植清单(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "SKILL.md"), "---\nname: demo-skill\ndescription: Export demo.\n---\n\n# Demo\n\npassword: \"supersecretvalue\"\n")
	writeFile(t, filepath.Join(root, "references", "guide.md"), "Use a relative path.\n")
	writeFile(t, filepath.Join(root, ".env"), "TOKEN=real\n")

	destination := filepath.Join(t.TempDir(), "skill.zip")
	if _, err := ExportDirectory(root, KindSkill, destination); err == nil {
		t.Fatal("export accepted a Skill package that contains .env")
	}

	os.Remove(filepath.Join(root, ".env"))
	exported, err := ExportDirectory(root, KindSkill, destination)
	if err != nil {
		t.Fatal(err)
	}
	if exported.Size == 0 || exported.ArchiveDigest == "" || exported.ContentDigest == "" {
		t.Fatalf("export = %#v", exported)
	}
	if !hasFile(exported.Files, "SKILL.md") || !hasFile(exported.Files, "references/guide.md") {
		t.Fatalf("files = %#v", exported.Files)
	}
	if len(exported.Warnings) == 0 {
		t.Fatal("hardcoded credential warning missing")
	}
	names := zipNames(t, destination)
	for _, name := range names {
		if name == ".env" || name == ".pem" {
			t.Fatalf("exported zip contains private file %s", name)
		}
	}
}

func TestExportDirectory_拒绝符号链接特殊文件和白名单外路径(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "SKILL.md"), "---\nname: demo-skill\ndescription: Export demo.\n---\n\n# Demo\n")
	if err := os.Symlink(filepath.Join(root, "SKILL.md"), filepath.Join(root, "references")); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportDirectory(root, KindSkill, filepath.Join(t.TempDir(), "link.zip")); err == nil {
		t.Fatal("export accepted a symlink")
	}
	os.Remove(filepath.Join(root, "references"))

	fifo := filepath.Join(root, "scripts")
	if err := os.MkdirAll(fifo, 0o700); err != nil {
		t.Fatal(err)
	}
	fifo = filepath.Join(fifo, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportDirectory(root, KindSkill, filepath.Join(t.TempDir(), "fifo.zip")); err == nil {
		t.Fatal("export accepted a FIFO")
	}
	os.Remove(fifo)

	writeFile(t, filepath.Join(root, "bin", "tool"), "#!/bin/sh\n")
	if _, err := ExportDirectory(root, KindSkill, filepath.Join(t.TempDir(), "bin.zip")); err == nil {
		t.Fatal("export accepted a path outside the allowlist")
	}
}

func TestExportDirectory_插件只导出可移植快照(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "plugin.json"), `{"name":"demo-plugin","version":"1.0.0","description":"Demo"}`)
	writeFile(t, filepath.Join(root, "skills", "demo-skill", "SKILL.md"), "---\nname: demo-skill\ndescription: Plugin skill.\n---\n\n# Demo\n")
	writeFile(t, filepath.Join(root, "notes.pem"), "-----BEGIN PRIVATE KEY-----\n")
	if _, err := ExportDirectory(root, KindPlugin, filepath.Join(t.TempDir(), "plugin.zip")); err == nil {
		t.Fatal("export accepted a private key file")
	}
}

func TestInspectArchive_拒绝路径穿越符号链接和秘密文件(t *testing.T) {
	traversal := filepath.Join(t.TempDir(), "traversal.zip")
	writeRawZip(t, traversal, map[string]string{"../evil.txt": "owned"})
	if err := InspectArchive(traversal, KindSkill); err == nil {
		t.Fatal("inspect accepted a traversal entry")
	}

	secret := filepath.Join(t.TempDir(), "secret.zip")
	writeRawZip(t, secret, map[string]string{".env": "TOKEN=real", "SKILL.md": "---\nname: demo-skill\n"})
	if err := InspectArchive(secret, KindSkill); err == nil {
		t.Fatal("inspect accepted .env")
	}

	link := filepath.Join(t.TempDir(), "link.zip")
	writeSymlinkZip(t, link, "SKILL.md", "outside")
	if err := InspectArchive(link, KindSkill); err == nil {
		t.Fatal("inspect accepted a symlink")
	}

	valid := filepath.Join(t.TempDir(), "valid.zip")
	writeRawZip(t, valid, map[string]string{"SKILL.md": "---\nname: demo-skill\ndescription: Demo.\n---\n\n# Demo\n"})
	if err := InspectArchive(valid, KindSkill); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeRawZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	output, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeSymlinkZip(t *testing.T, path, name, target string) {
	t.Helper()
	output, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	header := &zip.FileHeader{Name: name}
	header.SetMode(os.ModeSymlink | 0o777)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(target)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func zipNames(t *testing.T, path string) []string {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	return names
}
