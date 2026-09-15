// Package agentinstructions discovers bounded, workspace-scoped AGENTS.md guidance.
// It has no mutable workspace state and never executes instructions or file contents.
package agentinstructions

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	Filename       = "AGENTS.md"
	MaxFileBytes   = 64 << 10
	MaxTotalBytes  = 256 << 10
	MaxDirectories = 64
)

type Options struct {
	Home            string
	DefaultDir      string
	Workdir         string
	GlobalFile      string
	DisableAutoLoad bool
}

type File struct {
	Scope       string `json:"scope"`
	Path        string `json:"path"`
	Status      string `json:"status"`
	Content     string `json:"content,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	Reason      string `json:"reason,omitempty"`
	DuplicateOf string `json:"duplicate_of,omitempty"`
}

type Snapshot struct {
	AutoLoad      bool   `json:"auto_load"`
	Workdir       string `json:"workdir"`
	WorkspaceRoot string `json:"workspace_root"`
	Files         []File `json:"files"`
}

type candidate struct {
	scope, path, root string
	explicit          bool
}

type loadedFile struct {
	info os.FileInfo
	path string
}

// Load reads fresh content on each request. Missing optional files are normal;
// unreadable or invalid files are reported without including partial instructions.
func Load(ctx context.Context, options Options) (Snapshot, error) {
	snapshot := Snapshot{AutoLoad: !options.DisableAutoLoad, Workdir: options.Workdir, WorkspaceRoot: options.Workdir, Files: []File{}}
	if err := ctx.Err(); err != nil {
		return snapshot, err
	}
	if !filepath.IsAbs(options.Workdir) {
		return snapshot, errors.New("instruction workdir must be an absolute directory")
	}
	info, err := os.Stat(options.Workdir)
	if err != nil || !info.IsDir() {
		return snapshot, errors.New("instruction workdir must be an existing directory")
	}
	candidates := []candidate{}
	if options.GlobalFile != "" {
		if !filepath.IsAbs(options.GlobalFile) {
			return snapshot, errors.New("global instruction file must be absolute")
		}
		candidates = append(candidates, candidate{scope: "global", path: options.GlobalFile, root: filepath.Dir(options.GlobalFile), explicit: true})
	} else if !options.DisableAutoLoad && options.Home != "" {
		if !filepath.IsAbs(options.Home) {
			return snapshot, errors.New("instruction home must be absolute")
		}
		candidates = append(candidates, candidate{scope: "global", path: filepath.Join(options.Home, Filename), root: options.Home})
	}
	if !options.DisableAutoLoad {
		dirs, err := workspaceDirectories(ctx, options.Workdir, options.DefaultDir)
		if err != nil {
			return snapshot, err
		}
		snapshot.WorkspaceRoot = dirs[0]
		for _, dir := range dirs {
			candidates = append(candidates, candidate{scope: "workspace", path: filepath.Join(dir, Filename), root: dirs[0]})
		}
	}
	seen := []loadedFile{}
	remaining := int64(MaxTotalBytes)
	for _, source := range candidates {
		if err := ctx.Err(); err != nil {
			return snapshot, err
		}
		file, info := readCandidate(source)
		if file.Status == "loaded" {
			for _, prior := range seen {
				if os.SameFile(prior.info, info) {
					file.Status, file.Content, file.DuplicateOf = "duplicate", "", prior.path
					break
				}
			}
			if file.Status == "loaded" {
				if file.SizeBytes > remaining {
					file.Status, file.Content, file.Reason = "skipped", "", "total_size_limit"
				} else {
					remaining -= file.SizeBytes
					seen = append(seen, loadedFile{info: info, path: file.Path})
				}
			}
		}
		snapshot.Files = append(snapshot.Files, file)
	}
	return snapshot, ctx.Err()
}

// Only repository ancestors (or ancestors inside the configured default directory)
// are eligible. We never read parent AGENTS.md files outside this boundary.
func workspaceDirectories(ctx context.Context, workdir, defaultDir string) ([]string, error) {
	boundary := ""
	if filepath.IsAbs(defaultDir) && within(defaultDir, workdir) {
		boundary = filepath.Clean(defaultDir)
	}
	root := workdir
	found := false
	for dir, count := workdir, 0; ; dir, count = filepath.Dir(dir), count+1 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if count >= MaxDirectories {
			return nil, errors.New("workspace instruction discovery exceeds directory limit")
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			root, found = dir, true
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("cannot inspect workspace boundary: %w", err)
		}
		if boundary != "" {
			// filepath.Rel applies the host's path equality rules, including
			// case-insensitive drive and directory names on Windows.
			if rel, err := filepath.Rel(boundary, dir); err == nil && rel == "." {
				root, found = dir, true
				break
			}
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	if !found {
		return []string{workdir}, nil
	}
	dirs := []string{}
	for dir := workdir; ; dir = filepath.Dir(dir) {
		if len(dirs) >= MaxDirectories {
			return nil, errors.New("workspace instruction inheritance exceeds directory limit")
		}
		dirs = append(dirs, dir)
		if dir == root {
			break
		}
	}
	slices.Reverse(dirs)
	return dirs, nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func readCandidate(source candidate) (File, os.FileInfo) {
	file := File{Scope: source.scope, Path: filepath.Clean(source.path)}
	// An explicitly configured file retains the existing symlink semantics.
	// Automatic discovery never follows a leaf symlink into an unrelated file.
	if source.explicit {
		realPath, err := filepath.EvalSymlinks(source.path)
		if err != nil {
			return failedFile(file, err), nil
		}
		source.path, source.root = realPath, filepath.Dir(realPath)
	}
	root, err := os.OpenRoot(source.root)
	if err != nil {
		return failedFile(file, err), nil
	}
	defer root.Close()
	rel, err := filepath.Rel(source.root, source.path)
	if err != nil || !within(source.root, source.path) {
		file.Status, file.Reason = "skipped", "outside_scope"
		return file, nil
	}
	before, err := root.Lstat(rel)
	if err != nil {
		return failedFile(file, err), nil
	}
	if !before.Mode().IsRegular() {
		file.Status, file.Reason = "skipped", "not_regular_file"
		return file, nil
	}
	if before.Size() > MaxFileBytes {
		file.Status, file.Reason, file.SizeBytes = "skipped", "file_size_limit", before.Size()
		return file, nil
	}
	opened, err := root.OpenFile(rel, instructionOpenFlags(), 0)
	if err != nil {
		return failedFile(file, err), nil
	}
	defer opened.Close()
	after, err := opened.Stat()
	if err != nil {
		return failedFile(file, err), nil
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		file.Status, file.Reason = "skipped", "file_changed_during_read"
		return file, nil
	}
	data, err := io.ReadAll(io.LimitReader(opened, MaxFileBytes+1))
	if err != nil {
		return failedFile(file, err), nil
	}
	file.SizeBytes = int64(len(data))
	if len(data) > MaxFileBytes {
		file.Status, file.Reason = "skipped", "file_size_limit"
		return file, nil
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		file.Status, file.Reason = "skipped", "invalid_utf8_text"
		return file, nil
	}
	file.Content = strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	if file.Content == "" {
		file.Status = "empty"
		return file, nil
	}
	file.Status, file.SHA256 = "loaded", fmt.Sprintf("%x", sha256.Sum256(data))
	return file, after
}

func failedFile(file File, err error) File {
	file.Status, file.Reason = "error", "read_failed"
	if errors.Is(err, os.ErrNotExist) {
		file.Status, file.Reason = "not_found", ""
	} else if errors.Is(err, os.ErrPermission) {
		file.Reason = "permission_denied"
	}
	return file
}

// Text labels provenance and scope rather than promoting repository text into
// unqualified server/operator instructions. File errors remain visible to clients.
func (s Snapshot) Text() string {
	var out strings.Builder
	for _, file := range s.Files {
		switch file.Status {
		case "loaded":
			fmt.Fprintf(&out, "\n\n### %s guidance\nSource: %q\n", file.Scope, file.Path)
			if file.Scope == "workspace" {
				fmt.Fprintf(&out, "Scope: %q and its descendants. Refines global guidance; does not override global safety requirements or the client's higher-priority instructions.\n", filepath.Dir(file.Path))
			}
			fmt.Fprintf(&out, "SHA-256: %s\n\n%s", file.SHA256, file.Content)
		case "error", "skipped":
			fmt.Fprintf(&out, "\n\nInstruction file %q was not loaded (%s). Do not claim its rules were applied.", file.Path, file.Reason)
		}
	}
	return strings.TrimSpace(out.String())
}
