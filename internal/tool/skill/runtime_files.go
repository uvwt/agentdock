package skill

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	runtimeSkillMaxFiles        = 200
	runtimeSkillMaxDepth        = 8
	runtimeSkillFilePreviewSize = 256 * 1024
)

type runtimeSkillFile struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	SizeBytes int64  `json:"size_bytes"`
	UpdatedAt string `json:"updated_at"`
}

// RuntimeSkillFiles returns the current managed Skill package file list.
func (s *Service) RuntimeSkillFiles(skill string) (Result, error) {
	resolved, release, err := s.Acquire(context.Background(), ManagedSkillRef(strings.TrimSpace(skill)))
	if err != nil {
		return nil, err
	}
	defer release()
	files, err := collectRuntimeSkillFiles(resolved.Root)
	if err != nil {
		return nil, err
	}
	digest, err := managedContentDigest(resolved.Root)
	if err != nil {
		return nil, err
	}
	return Result{
		"action": "files", "skill": resolved.Name, "skill_ref": resolved.SkillRef,
		"content_digest": digest,
		"files":          files, "count": len(files), "source": runtimeAPISource,
	}, nil
}

// RuntimeSkillFile only reads regular UTF-8 text files inside current managed Skill content.
func (s *Service) RuntimeSkillFile(skill, relativePath string) (Result, error) {
	resolved, release, err := s.Acquire(context.Background(), ManagedSkillRef(strings.TrimSpace(skill)))
	if err != nil {
		return nil, err
	}
	defer release()

	cleanPath, err := cleanRuntimeSkillFilePath(relativePath)
	if err != nil {
		return nil, err
	}
	if isPrivateRuntimeSkillFile(cleanPath) {
		return nil, toolErrorDetails("SKILL_FILE_NOT_FOUND", "skill file not found", "not_found", nil)
	}

	resolvedRoot, err := filepath.EvalSymlinks(resolved.Root)
	if err != nil {
		return nil, toolErrorDetails("SKILL_PACKAGE_UNAVAILABLE", "managed Skill package directory is unavailable", "runtime", map[string]any{"skill_ref": resolved.SkillRef})
	}
	if err := rejectRuntimeSkillSymlinkPath(resolvedRoot, cleanPath); err != nil {
		return nil, err
	}
	target := filepath.Join(resolvedRoot, filepath.FromSlash(cleanPath))
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, toolErrorDetails("SKILL_FILE_NOT_FOUND", "skill file not found", "not_found", map[string]any{"path": cleanPath})
	}
	inside, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
		return nil, toolErrorDetails("INVALID_SKILL_FILE", "skill file path escapes the managed package", "validation", map[string]any{"path": cleanPath})
	}
	info, err := os.Stat(resolvedTarget)
	if err != nil || !info.Mode().IsRegular() {
		return nil, toolErrorDetails("SKILL_FILE_NOT_FOUND", "skill file not found", "not_found", map[string]any{"path": cleanPath})
	}

	file, err := os.Open(resolvedTarget)
	if err != nil {
		return nil, toolErrorDetails("SKILL_FILE_READ_FAILED", "failed to read skill file", "runtime", map[string]any{"path": cleanPath})
	}
	defer file.Close()
	limit := int64(runtimeSkillFilePreviewSize)
	buffer, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, toolErrorDetails("SKILL_FILE_READ_FAILED", "failed to read skill file", "runtime", map[string]any{"path": cleanPath})
	}
	truncated := int64(len(buffer)) > limit
	if truncated {
		buffer = buffer[:limit]
	}
	if bytes.IndexByte(buffer, 0) >= 0 || !utf8.Valid(buffer) {
		return nil, toolErrorDetails("SKILL_FILE_NOT_TEXT", "skill file is not UTF-8 text", "validation", map[string]any{"path": cleanPath})
	}
	digest, err := managedContentDigest(resolved.Root)
	if err != nil {
		return nil, err
	}
	return Result{
		"action": "file", "skill": resolved.Name, "skill_ref": resolved.SkillRef,
		"content_digest": digest,
		"file": map[string]any{
			"path": cleanPath, "kind": runtimeSkillFileKind(cleanPath),
			"size_bytes": info.Size(), "updated_at": info.ModTime().UTC().Format(time.RFC3339Nano),
			"content": string(buffer), "truncated": truncated,
		},
		"source": runtimeAPISource,
	}, nil
}

func collectRuntimeSkillFiles(root string) ([]runtimeSkillFile, error) {
	root = filepath.Clean(root)
	baseDepth := strings.Count(root, string(os.PathSeparator))
	files := make([]runtimeSkillFile, 0, 16)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		depth := strings.Count(filepath.Clean(path), string(os.PathSeparator)) - baseDepth
		if entry.IsDir() && depth > runtimeSkillMaxDepth {
			return fs.SkipDir
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if isPrivateRuntimeSkillFile(relative) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		files = append(files, runtimeSkillFile{
			Path: relative, Kind: runtimeSkillFileKind(relative), SizeBytes: info.Size(),
			UpdatedAt: info.ModTime().UTC().Format(time.RFC3339Nano),
		})
		if len(files) >= runtimeSkillMaxFiles {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, toolErrorDetails("SKILL_FILES_READ_FAILED", "failed to list skill package files", "runtime", map[string]any{"reason": err.Error()})
	}
	sort.SliceStable(files, func(i, j int) bool {
		left, right := runtimeSkillFileRank(files[i]), runtimeSkillFileRank(files[j])
		if left != right {
			return left < right
		}
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func cleanRuntimeSkillFilePath(path string) (string, error) {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if path == "" || strings.HasPrefix(path, "/") {
		return "", toolErrorDetails("INVALID_SKILL_FILE", "invalid skill file path", "validation", map[string]any{"path": path})
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return "", toolErrorDetails("INVALID_SKILL_FILE", "invalid skill file path", "validation", map[string]any{"path": path})
		}
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", toolErrorDetails("INVALID_SKILL_FILE", "invalid skill file path", "validation", map[string]any{"path": path})
	}
	return clean, nil
}

func rejectRuntimeSkillSymlinkPath(root, relativePath string) error {
	current := root
	for _, segment := range strings.Split(filepath.FromSlash(relativePath), string(os.PathSeparator)) {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil {
			return toolErrorDetails("SKILL_FILE_NOT_FOUND", "skill file not found", "not_found", map[string]any{"path": relativePath})
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return toolErrorDetails("INVALID_SKILL_FILE", "skill file path contains a symbolic link", "validation", map[string]any{"path": relativePath})
		}
	}
	return nil
}

func isPrivateRuntimeSkillFile(path string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		name := strings.ToLower(strings.TrimSpace(segment))
		if name == "" || strings.HasPrefix(name, ".") || name == "_meta.json" {
			return true
		}
	}
	return false
}

func runtimeSkillFileKind(path string) string {
	name := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case name == "skill.md" || name == "readme.md":
		return "doc"
	case name == "manifest.json" || name == "package.json" || name == "skill.json":
		return "manifest"
	case ext == ".go" || ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".py" || ext == ".sh":
		return "code"
	case ext == ".json" || ext == ".yaml" || ext == ".yml" || ext == ".toml":
		return "config"
	default:
		return "asset"
	}
}

func runtimeSkillFileRank(file runtimeSkillFile) int {
	if strings.EqualFold(file.Path, "SKILL.md") {
		return 0
	}
	switch file.Kind {
	case "doc":
		return 1
	case "code":
		return 2
	case "config", "manifest":
		return 3
	default:
		return 4
	}
}
