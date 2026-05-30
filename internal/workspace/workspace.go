package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Workspace struct {
	root       string
	defaultCWD string
	hostPaths  bool
	mu         sync.RWMutex
}

type Path struct {
	Display string `json:"display"`
	Abs     string `json:"-"`
	Exists  bool   `json:"exists"`
}

func New(root string, hostPaths bool) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	realRoot, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(realRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace root is not a directory: %s", realRoot)
	}
	if realRoot == string(filepath.Separator) {
		return nil, errors.New("refusing to use filesystem root as workspace")
	}
	return &Workspace{root: realRoot, defaultCWD: realRoot, hostPaths: hostPaths}, nil
}

func (w *Workspace) Root() string { return w.root }

func (w *Workspace) HostPaths() bool { return w.hostPaths }

func (w *Workspace) DefaultCWD() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.defaultCWD
}

func (w *Workspace) DefaultDisplay() string {
	display, err := w.Relative(w.DefaultCWD())
	if err != nil || display == "" {
		return "."
	}
	return display
}

func (w *Workspace) SetDefaultCWD(raw string) (string, error) {
	resolved, err := w.ResolveExisting(raw)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved.Abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", raw)
	}
	w.mu.Lock()
	w.defaultCWD = resolved.Abs
	w.mu.Unlock()
	return resolved.Display, nil
}

func (w *Workspace) ResolveExisting(raw string) (Path, error) {
	return w.resolve(raw, true)
}

func (w *Workspace) ResolveForWrite(raw string) (Path, error) {
	return w.resolve(raw, false)
}

func (w *Workspace) resolveHost(raw string, mustExist bool) (Path, error) {
	if raw == "" {
		raw = "."
	}
	if strings.Contains(raw, string(rune(0))) {
		return Path{}, errors.New("path contains invalid byte")
	}
	var candidate string
	if strings.HasPrefix(raw, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return Path{}, err
		}
		if raw == "~" {
			candidate = home
		} else if strings.HasPrefix(raw, "~/") {
			candidate = filepath.Join(home, raw[2:])
		} else {
			return Path{}, errors.New("unsupported home path; use ~/path or an absolute path")
		}
	} else if filepath.IsAbs(raw) {
		candidate = raw
	} else {
		candidate = filepath.Join(w.DefaultCWD(), filepath.FromSlash(raw))
	}
	candidate = filepath.Clean(candidate)
	if mustExist {
		realPath, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return Path{}, err
		}
		display, _ := w.Relative(realPath)
		return Path{Display: display, Abs: realPath, Exists: true}, nil
	}
	parent := candidate
	for {
		if info, err := os.Lstat(parent); err == nil && info.IsDir() {
			break
		}
		next := filepath.Dir(parent)
		if next == parent {
			return Path{}, fmt.Errorf("parent directory not found: %s", raw)
		}
		parent = next
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return Path{}, err
	}
	relFromParent, err := filepath.Rel(parent, candidate)
	if err != nil {
		return Path{}, err
	}
	realPath := filepath.Join(realParent, relFromParent)
	display, _ := w.Relative(realPath)
	_, statErr := os.Stat(realPath)
	return Path{Display: display, Abs: realPath, Exists: statErr == nil}, nil
}

func (w *Workspace) resolve(raw string, mustExist bool) (Path, error) {
	if w.hostPaths {
		return w.resolveHost(raw, mustExist)
	}
	clean, err := Clean(raw)
	if err != nil {
		return Path{}, err
	}
	candidate := filepath.Join(w.DefaultCWD(), filepath.FromSlash(clean))
	if mustExist {
		realPath, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return Path{}, err
		}
		if !w.inside(realPath) {
			return Path{}, fmt.Errorf("path escapes workspace: %s", raw)
		}
		display, _ := w.Relative(realPath)
		return Path{Display: display, Abs: realPath, Exists: true}, nil
	}
	parent := candidate
	for {
		if info, err := os.Lstat(parent); err == nil && info.IsDir() {
			break
		}
		next := filepath.Dir(parent)
		if next == parent {
			return Path{}, fmt.Errorf("parent directory not found: %s", raw)
		}
		parent = next
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return Path{}, err
	}
	if !w.inside(realParent) {
		return Path{}, fmt.Errorf("path escapes workspace: %s", raw)
	}
	relFromParent, err := filepath.Rel(parent, candidate)
	if err != nil {
		return Path{}, err
	}
	realPath := filepath.Join(realParent, relFromParent)
	display, _ := w.Relative(realPath)
	_, statErr := os.Stat(realPath)
	return Path{Display: display, Abs: realPath, Exists: statErr == nil}, nil
}

func (w *Workspace) Relative(abs string) (string, error) {
	rel, err := filepath.Rel(w.root, abs)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return ".", nil
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return filepath.Clean(abs), nil
	}
	return filepath.ToSlash(rel), nil
}

func (w *Workspace) inside(path string) bool {
	rel, err := filepath.Rel(w.root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

func Clean(raw string) (string, error) {
	if raw == "" {
		raw = "."
	}
	if strings.Contains(raw, string(rune(0))) {
		return "", errors.New("path contains invalid byte")
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "~") {
		return "", errors.New("absolute paths are denied; use workspace-relative paths such as '.' instead of '/workspace'")
	}
	clean := filepath.ToSlash(filepath.Clean(raw))
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return "", errors.New("path escapes workspace")
		}
	}
	return clean, nil
}

func Hidden(name string) bool { return strings.HasPrefix(name, ".") }
