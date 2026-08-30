package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestRemovedSkillRuntimeToolsAreUnavailable(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock")}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"skill_read", "skill_run", "skill_env_manage"} {
		_, err := rt.Call(context.Background(), name, map[string]any{})
		var toolErr *ToolError
		if !errors.As(err, &toolErr) || toolErr.Code != "UNKNOWN_TOOL" {
			t.Fatalf("%s should be unavailable, got %T %v", name, err, err)
		}
	}
}
