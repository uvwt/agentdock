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
	mu         sync.RWMutex
}

type Path struct {
	Display string `json:"display"`
	Abs     string `json:"-"`
	Exists  bool   `json:"exists"`
}

// New builds the single Host path resolver.
func New(root string) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
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
		return nil, fmt.Errorf("default directory is not a directory: %s", realRoot)
	}
	if filepath.Dir(realRoot) == realRoot {
		return nil, errors.New("refusing to use filesystem root as default directory")
	}
	return &Workspace{root: realRoot, defaultCWD: realRoot}, nil
}

func (w *Workspace) Root() string { return w.root }

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

func (w *Workspace) resolve(raw string, mustExist bool) (Path, error) {
	if raw == "" {
		raw = "."
	}
	if strings.Contains(raw, string(rune(0))) {
		return Path{}, errors.New("path contains invalid byte")
	}
	var candidate string
	switch {
	case raw == "~":
		home, err := userHomeDir()
		if err != nil {
			return Path{}, err
		}
		candidate = home
	case strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, `~\`):
		home, err := userHomeDir()
		if err != nil {
			return Path{}, err
		}
		candidate = filepath.Join(home, raw[2:])
	case strings.HasPrefix(raw, "~"):
		return Path{}, errors.New("unsupported home path; use ~/path or an absolute path")
	case filepath.IsAbs(raw):
		candidate = raw
	default:
		candidate = filepath.Join(w.DefaultCWD(), filepath.FromSlash(raw))
	}
	candidate = filepath.Clean(candidate)
	if mustExist {
		realPath, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return Path{}, err
		}
		return Path{Display: w.displayPath(realPath), Abs: realPath, Exists: true}, nil
	}
	parent := candidate
	for {
		if info, err := os.Stat(parent); err == nil && info.IsDir() {
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
	_, statErr := os.Stat(realPath)
	return Path{Display: w.displayPath(realPath), Abs: realPath, Exists: statErr == nil}, nil
}

func userHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", errors.New("resolve user home: home directory is empty")
	}
	return home, nil
}

func (w *Workspace) displayPath(abs string) string {
	display, err := w.Relative(abs)
	if err != nil || display == "" {
		return filepath.Clean(abs)
	}
	return display
}

func (w *Workspace) Relative(abs string) (string, error) {
	rel, err := filepath.Rel(w.root, abs)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return ".", nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return filepath.Clean(abs), nil
	}
	return filepath.ToSlash(rel), nil
}

// Clean remains a lexical helper for patch parsing. It is not a security boundary.
func Clean(raw string) (string, error) {
	if raw == "" {
		raw = "."
	}
	if strings.Contains(raw, string(rune(0))) {
		return "", errors.New("path contains invalid byte")
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "~") {
		return filepath.Clean(raw), nil
	}
	return filepath.ToSlash(filepath.Clean(raw)), nil
}

func Hidden(name string) bool { return strings.HasPrefix(name, ".") }
