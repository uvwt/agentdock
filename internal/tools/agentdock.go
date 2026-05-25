package tools

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/uvwt/coding-tools-mcp-go/internal/workspace"
)

type controlPath struct {
	Abs     string
	Display string
}

func (r *Runtime) agentDockRoot() (controlPath, error) {
	if r.cfg.AgentDockDir == "" {
		return controlPath{}, toolError("AGENT_DOCK_DISABLED", "AgentDock is not configured", "validation")
	}
	if filepath.IsAbs(r.cfg.AgentDockDir) {
		abs := filepath.Clean(r.cfg.AgentDockDir)
		return controlPath{Abs: abs, Display: abs}, nil
	}
	p, err := r.ws.ResolveForWrite(r.cfg.AgentDockDir)
	if err != nil {
		return controlPath{}, err
	}
	return fromWorkspacePath(p), nil
}

func (r *Runtime) resolveControlForWrite(path string) (controlPath, error) {
	return r.resolveAgentDockPath(path, false)
}

func (r *Runtime) resolveControlExisting(path string) (controlPath, error) {
	p, err := r.resolveAgentDockPath(path, true)
	if err != nil {
		return controlPath{}, err
	}
	if _, err := os.Stat(p.Abs); err != nil {
		return controlPath{}, err
	}
	return p, nil
}

func (r *Runtime) resolveAgentDockPath(path string, existing bool) (controlPath, error) {
	root, err := r.agentDockRoot()
	if err != nil {
		return controlPath{}, err
	}
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(root.Abs, path))
	}
	if !isWithinRoot(root.Abs, abs) {
		return controlPath{}, toolError("PATH_OUTSIDE_AGENT_DOCK", "path escapes AgentDock directory", "validation")
	}
	if existing {
		if _, err := os.Stat(abs); err != nil {
			return controlPath{}, err
		}
	}
	return controlPath{Abs: abs, Display: displayControlPath(root, abs)}, nil
}

func fromWorkspacePath(p workspace.Path) controlPath {
	return controlPath{Abs: p.Abs, Display: p.Display}
}

func displayControlPath(root controlPath, abs string) string {
	if root.Display == root.Abs {
		return abs
	}
	rel, err := filepath.Rel(root.Abs, abs)
	if err != nil || rel == "." {
		return root.Display
	}
	return filepath.ToSlash(filepath.Join(root.Display, rel))
}

func isWithinRoot(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	if candidate == root {
		return true
	}
	return strings.HasPrefix(candidate, root+string(os.PathSeparator))
}
