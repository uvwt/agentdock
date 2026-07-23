package media

import (
	"os"
	"path/filepath"
	"strings"
)

type ControlPath struct {
	Abs     string
	Display string
}

func (s *Service) agentDockRoot() (ControlPath, error) {
	if s.cfg.AgentDockHome == "" {
		return ControlPath{}, toolError("AGENT_DOCK_DISABLED", "AgentDock home is not configured", "validation")
	}
	abs := filepath.Clean(s.cfg.AgentDockHome)
	return ControlPath{Abs: abs, Display: abs}, nil
}

func (s *Service) resolveControlForWrite(path string) (ControlPath, error) {
	return s.resolveAgentDockPath(path, false)
}

func (s *Service) resolveControlExisting(path string) (ControlPath, error) {
	p, err := s.resolveAgentDockPath(path, true)
	if err != nil {
		return ControlPath{}, err
	}
	if _, err := os.Stat(p.Abs); err != nil {
		return ControlPath{}, err
	}
	return p, nil
}

func (s *Service) resolveAgentDockPath(path string, existing bool) (ControlPath, error) {
	root, err := s.agentDockRoot()
	if err != nil {
		return ControlPath{}, err
	}
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(root.Abs, path))
	}
	if !isWithinRoot(root.Abs, abs) {
		return ControlPath{}, toolError("PATH_OUTSIDE_AGENT_DOCK", "path escapes AgentDock directory", "validation")
	}
	if existing {
		if _, err := os.Stat(abs); err != nil {
			return ControlPath{}, err
		}
	}
	return ControlPath{Abs: abs, Display: displayControlPath(root, abs)}, nil
}

func displayControlPath(root ControlPath, abs string) string {
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
