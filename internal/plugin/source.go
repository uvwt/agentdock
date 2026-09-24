package plugin

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	skills "github.com/uvwt/agentdock/internal/skill"
)

const (
	maxPluginArchiveBytes   = int64(64 << 20)
	maxPluginExtractedBytes = int64(256 << 20)
	maxPluginArchiveFiles   = 10000
)

type stagedPluginSource struct {
	Root    string
	Cleanup func()
}

// stagePluginSource snapshots a local Plugin directory or extracts a local
// ZIP into AgentDock-owned temporary storage. Network acquisition stays outside
// Plugin Core; local Portable/OpenAI/Claude format normalization runs after this
// transport-level safety boundary.
func (m *Manager) stagePluginSource(source string) (stagedPluginSource, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return stagedPluginSource{}, pluginError("PLUGIN_SOURCE_INVALID", "source", errors.New("Plugin source is required"))
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return stagedPluginSource{}, pluginError("PLUGIN_SOURCE_INVALID", "source", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return stagedPluginSource{}, pluginError("PLUGIN_SOURCE_INVALID", "source", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return stagedPluginSource{}, pluginError("PLUGIN_SOURCE_INVALID", "source", errors.New("Plugin source must not be a symlink"))
	}
	switch {
	case info.IsDir():
		return m.stagePluginDirectory(absolute)
	case info.Mode().IsRegular() && strings.EqualFold(filepath.Ext(absolute), ".zip"):
		return m.stagePluginArchive(absolute, info.Size())
	default:
		return stagedPluginSource{}, pluginError("PLUGIN_SOURCE_INVALID", "source", errors.New("Plugin source must be a local directory or ZIP archive"))
	}
}

func (m *Manager) stagePluginDirectory(source string) (stagedPluginSource, error) {
	temp, err := m.store.TempPath("local-source")
	if err != nil {
		return stagedPluginSource{}, err
	}
	cleanup := func() { _ = os.RemoveAll(temp) }
	snapshot := filepath.Join(temp, "tree")
	if err := snapshotPluginTree(source, snapshot, maxPluginExtractedBytes, maxPluginArchiveFiles); err != nil {
		cleanup()
		return stagedPluginSource{}, pluginError("PLUGIN_SOURCE_INVALID", "source.snapshot", err)
	}
	return stagedPluginSource{Root: snapshot, Cleanup: cleanup}, nil
}

func (m *Manager) stagePluginArchive(source string, size int64) (stagedPluginSource, error) {
	if size > maxPluginArchiveBytes {
		return stagedPluginSource{}, pluginError("PLUGIN_SOURCE_INVALID", "source.archive.size", fmt.Errorf("archive exceeds %d bytes", maxPluginArchiveBytes))
	}
	temp, err := m.store.TempPath("archive-source")
	if err != nil {
		return stagedPluginSource{}, err
	}
	cleanup := func() { _ = os.RemoveAll(temp) }
	extracted := filepath.Join(temp, "tree")
	if err := os.MkdirAll(extracted, 0o700); err != nil {
		cleanup()
		return stagedPluginSource{}, err
	}
	if err := extractPluginZip(source, extracted); err != nil {
		cleanup()
		return stagedPluginSource{}, pluginError("PLUGIN_SOURCE_INVALID", "source.archive.extract", err)
	}
	root, err := selectPluginSourceRoot(extracted)
	if err != nil {
		cleanup()
		return stagedPluginSource{}, pluginError("PLUGIN_SOURCE_INVALID", "source.archive.root", err)
	}
	return stagedPluginSource{Root: root, Cleanup: cleanup}, nil
}

func extractPluginZip(path, destination string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	if len(reader.File) > maxPluginArchiveFiles {
		return fmt.Errorf("Plugin archive exceeds %d entries", maxPluginArchiveFiles)
	}
	var total int64
	for _, entry := range reader.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if name == "" {
			continue
		}
		archivePath := filepath.FromSlash(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
		if !filepath.IsLocal(archivePath) || filepath.Clean(archivePath) == "." {
			return fmt.Errorf("archive path %q is not local", entry.Name)
		}
		relative, err := cleanPluginRelativePath(name)
		if err != nil {
			return fmt.Errorf("archive path %q: %w", entry.Name, err)
		}
		if filepath.Clean(archivePath) != filepath.FromSlash(relative) {
			return fmt.Errorf("archive path %q has ambiguous normalization", entry.Name)
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Plugin archive contains symlink %q", entry.Name)
		}
		if skills.IsIgnoredPackageMetadataPath(relative) {
			continue
		}
		target := filepath.Join(destination, archivePath)
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("Plugin archive contains special file %q", entry.Name)
		}
		total += int64(entry.UncompressedSize64)
		if total > maxPluginExtractedBytes {
			return fmt.Errorf("Plugin archive exceeds %d extracted bytes", maxPluginExtractedBytes)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		input, err := entry.Open()
		if err != nil {
			return err
		}
		mode := entry.Mode().Perm() & 0o755
		if mode == 0 {
			mode = 0o600
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			input.Close()
			return err
		}
		_, copyErr := io.Copy(output, io.LimitReader(input, int64(entry.UncompressedSize64)+1))
		closeOut := output.Close()
		closeIn := input.Close()
		if copyErr != nil || closeOut != nil || closeIn != nil {
			return errors.Join(copyErr, closeOut, closeIn)
		}
	}
	return nil
}

func cleanPluginRelativePath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || strings.HasPrefix(value, "/") {
		return "", errors.New("path must be a non-empty relative path")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(filepath.FromSlash(clean)) {
		return "", errors.New("path escapes source root")
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("path contains an invalid segment")
		}
	}
	return clean, nil
}

// SnapshotRuntimePackage copies one already-installed Plugin package into
// AgentDock-owned temporary storage for writable stdio runtime use. The
// reviewed package itself remains immutable, so runtime-generated files do not
// invalidate the persisted package digest.
func (s *Store) SnapshotRuntimePackage(packageRoot string) (string, error) {
	packageRoot = filepath.Clean(strings.TrimSpace(packageRoot))
	relative, err := filepath.Rel(s.pluginRoot, packageRoot)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("Plugin runtime source must be inside installed Plugin storage")
	}

	runtimeRoot, err := s.TempPath("mcp-runtime")
	if err != nil {
		return "", err
	}
	if err := snapshotPluginTree(packageRoot, runtimeRoot, maxPluginExtractedBytes, maxPluginArchiveFiles); err != nil {
		_ = os.RemoveAll(runtimeRoot)
		return "", err
	}
	return runtimeRoot, nil
}

func snapshotPluginTree(source, destination string, maxBytes int64, maxFiles int) error {
	source = filepath.Clean(source)
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return err
	}
	var total int64
	entries := 0
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || !filepath.IsLocal(relative) || relative == "." {
			return fmt.Errorf("invalid Plugin source path %q", path)
		}
		info, err := validatePluginSnapshotPath(root, relative, entry.IsDir())
		if err != nil {
			return fmt.Errorf("unsafe Plugin source path %q: %w", path, err)
		}
		if skills.IsIgnoredPackageMetadataPath(filepath.ToSlash(relative)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		entries++
		if entries > maxFiles {
			return fmt.Errorf("Plugin source exceeds %d entries", maxFiles)
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		remaining := maxBytes - total
		if remaining < 0 {
			return fmt.Errorf("Plugin source exceeds %d bytes", maxBytes)
		}
		mode := info.Mode().Perm() & 0o755
		if mode == 0 {
			mode = 0o600
		}
		copied, err := snapshotPluginRegularFile(root, relative, target, mode, remaining)
		if err != nil {
			return err
		}
		total += copied
		if total > maxBytes {
			return fmt.Errorf("Plugin source exceeds %d bytes", maxBytes)
		}
		return nil
	})
}

func validatePluginSnapshotPath(root *os.Root, relative string, wantDirectory bool) (os.FileInfo, error) {
	clean := filepath.Clean(relative)
	if !filepath.IsLocal(clean) || clean == "." {
		return nil, errors.New("path is not local to the Plugin source")
	}
	parts := strings.Split(clean, string(filepath.Separator))
	current := ""
	var info os.FileInfo
	for index, part := range parts {
		if current == "" {
			current = part
		} else {
			current = filepath.Join(current, part)
		}
		var err error
		info, err = root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("path contains symlink component %q", filepath.ToSlash(current))
		}
		if index < len(parts)-1 && !info.IsDir() {
			return nil, fmt.Errorf("path component %q is not a directory", filepath.ToSlash(current))
		}
	}
	if wantDirectory {
		if info == nil || !info.IsDir() {
			return nil, errors.New("path is not a directory")
		}
	} else if info == nil || !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	return info, nil
}

func snapshotPluginRegularFile(root *os.Root, relative, destination string, mode os.FileMode, remaining int64) (int64, error) {
	before, err := validatePluginSnapshotPath(root, relative, false)
	if err != nil {
		return 0, err
	}
	in, err := root.Open(relative)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	opened, err := in.Stat()
	if err != nil {
		return 0, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return 0, errors.New("Plugin source file changed while opening snapshot")
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return 0, err
	}
	copied, copyErr := io.Copy(out, io.LimitReader(in, remaining+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copied, copyErr
	}
	if closeErr != nil {
		return copied, closeErr
	}
	if copied > remaining {
		return copied, fmt.Errorf("Plugin source exceeds byte limit")
	}
	afterOpen, err := in.Stat()
	if err != nil {
		return copied, err
	}
	afterPath, err := validatePluginSnapshotPath(root, relative, false)
	if err != nil {
		return copied, err
	}
	if !os.SameFile(opened, afterOpen) || !os.SameFile(opened, afterPath) ||
		opened.Size() != afterOpen.Size() || opened.ModTime() != afterOpen.ModTime() || opened.Mode() != afterOpen.Mode() {
		return copied, errors.New("Plugin source file changed while creating snapshot")
	}
	return copied, nil
}

func selectPluginSourceRoot(root string) (string, error) {
	root = filepath.Clean(root)
	if hasPluginLayoutMarker(root) {
		return root, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var onlyDir string
	for _, entry := range entries {
		if skills.IsIgnoredPackageMetadataPath(entry.Name()) {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") && !entry.IsDir() {
			continue
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return root, nil
		}
		if onlyDir != "" {
			return root, nil
		}
		onlyDir = entry.Name()
	}
	if onlyDir != "" {
		candidate := filepath.Join(root, onlyDir)
		if hasPluginLayoutMarker(candidate) {
			return candidate, nil
		}
	}
	return root, nil
}

func hasPluginLayoutMarker(root string) bool {
	for _, path := range []string{
		"plugin.json",
		filepath.Join(".codex-plugin", "plugin.json"),
		filepath.Join(".claude-plugin", "plugin.json"),
	} {
		info, err := os.Lstat(filepath.Join(root, path))
		if err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return true
		}
	}
	return false
}
