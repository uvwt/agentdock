package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/uvwt/agentdock/internal/agentinstructions"
	"github.com/uvwt/agentdock/internal/config"
)

func newInstructionRuntime(t *testing.T, configure func(*config.Config)) *Runtime {
	t.Helper()
	cfg := config.Config{AgentDockHome: t.TempDir(), AgentDockDefaultDir: t.TempDir()}
	if configure != nil {
		configure(&cfg)
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})
	return rt
}

func writeInstructionFixture(t *testing.T, dir, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func instructionContext(t *testing.T, rt *Runtime, args map[string]any) capabilityContext {
	t.Helper()
	result, err := rt.Call(t.Context(), "agentdock_context", args)
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchestestOutputSchema(t, "agentdock_context", result)
	var got capabilityContext
	if err := remarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if got.InstructionFiles == nil {
		t.Fatal("instruction_files missing")
	}
	return got
}

func instructionBodies(snapshot *agentinstructions.Snapshot) string {
	var contents []string
	for _, file := range snapshot.Files {
		if file.Status == "loaded" {
			contents = append(contents, file.Content)
		}
	}
	return strings.Join(contents, "|")
}

func TestInstructionContextLoadsGlobalAndWorkspaceWithoutACP(t *testing.T) {
	rt := newInstructionRuntime(t, nil)
	writeInstructionFixture(t, rt.cfg.AgentDockHome, "global rule")
	writeInstructionFixture(t, rt.ws.Root(), "project rule")
	got := instructionContext(t, rt, nil)
	if body := instructionBodies(got.InstructionFiles); body != "global rule|project rule" {
		t.Fatalf("guidance=%q", body)
	}
	if got.ACP != nil {
		t.Fatal("autoload enabled ACP")
	}
	if !strings.Contains(strings.Join(got.Rules, "\n"), "status=loaded") {
		t.Fatal("context does not explain which files to apply")
	}
}

func TestInstructionContextWorkspaceSelectionIsRequestLocal(t *testing.T) {
	rt := newInstructionRuntime(t, nil)
	root := rt.ws.Root()
	writeInstructionFixture(t, rt.cfg.AgentDockHome, "global")
	writeInstructionFixture(t, root, "default")
	projectA, projectB := t.TempDir(), t.TempDir()
	writeInstructionFixture(t, projectA, "project-a")
	writeInstructionFixture(t, projectB, "project-b")
	for _, test := range []struct{ workdir, want string }{
		{projectA, "global|project-a"}, {projectB, "global|project-b"}, {"", "global|default"},
	} {
		got := instructionContext(t, rt, map[string]any{"workdir": test.workdir})
		if body := instructionBodies(got.InstructionFiles); body != test.want {
			t.Fatalf("workdir=%q: %q", test.workdir, body)
		}
		if rt.ws.DefaultCWD() != root {
			t.Fatal("request changed process-wide working directory")
		}
	}
	writeInstructionFixture(t, filepath.Join(root, "subdir"), "child")
	got := instructionContext(t, rt, map[string]any{"workdir": "subdir"})
	if body := instructionBodies(got.InstructionFiles); body != "global|default|child" {
		t.Fatalf("relative selection=%q", body)
	}
}

func TestInstructionContextConcurrentWorkspacesRemainIsolated(t *testing.T) {
	rt := newInstructionRuntime(t, nil)
	root := rt.ws.DefaultCWD()
	writeInstructionFixture(t, rt.cfg.AgentDockHome, "global")
	projects := []string{t.TempDir(), t.TempDir()}
	writeInstructionFixture(t, projects[0], "first")
	writeInstructionFixture(t, projects[1], "second")
	var wg sync.WaitGroup
	for index := range 16 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			result, err := rt.Call(t.Context(), "agentdock_context", map[string]any{"workdir": projects[index%2]})
			if err != nil {
				t.Error(err)
				return
			}
			var got capabilityContext
			if err := remarshal(result, &got); err != nil {
				t.Error(err)
				return
			}
			want := []string{"global|first", "global|second"}[index%2]
			if got.InstructionFiles == nil || instructionBodies(got.InstructionFiles) != want {
				t.Errorf("workspace context leaked across requests: %#v", got.InstructionFiles)
			}
		}(index)
	}
	wg.Wait()
	if rt.ws.DefaultCWD() != root {
		t.Fatal("concurrent selection changed default")
	}
}

func TestInstructionContextRefreshesExplicitFileInsteadOfStartupCopy(t *testing.T) {
	explicitDir := t.TempDir()
	path := writeInstructionFixture(t, explicitDir, "old-global-unique")
	rt := newInstructionRuntime(t, func(cfg *config.Config) { cfg.InstructionsFile = path })
	writeInstructionFixture(t, rt.cfg.AgentDockHome, "unused automatic global")
	writeInstructionFixture(t, rt.ws.Root(), "workspace")
	before := instructionContext(t, rt, nil)
	writeInstructionFixture(t, explicitDir, "new-global-unique")
	after := instructionContext(t, rt, nil)
	if body := instructionBodies(after.InstructionFiles); body != "new-global-unique|workspace" {
		t.Fatalf("refresh=%q", body)
	}
	if before.InstructionFiles.Files[0].SHA256 == after.InstructionFiles.Files[0].SHA256 {
		t.Fatal("explicit hash did not refresh")
	}
	if strings.Contains(strings.Join(after.Rules, "\n"), "old-global-unique") {
		t.Fatal("startup instructions leaked after refresh")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	removed := instructionContext(t, rt, nil)
	if removed.InstructionFiles.Files[0].Status != "not_found" {
		t.Fatal("removed explicit file remains loaded")
	}
}

func TestInstructionContextMissingSkippedAndDisabledStatesMatchSchema(t *testing.T) {
	rt := newInstructionRuntime(t, nil)
	missing := instructionContext(t, rt, nil)
	if len(missing.InstructionFiles.Files) != 2 || instructionBodies(missing.InstructionFiles) != "" {
		t.Fatalf("missing=%#v", missing.InstructionFiles)
	}
	writeInstructionFixture(t, rt.ws.Root(), strings.Repeat("x", agentinstructions.MaxFileBytes+1))
	skipped := instructionContext(t, rt, nil)
	if skipped.InstructionFiles.Files[1].Reason != "file_size_limit" {
		t.Fatal("oversized guidance not reported")
	}
	disabled := newInstructionRuntime(t, func(cfg *config.Config) { cfg.AgentsAutoLoadDisabled = true })
	writeInstructionFixture(t, disabled.cfg.AgentDockHome, "not loaded")
	writeInstructionFixture(t, disabled.ws.Root(), "not loaded")
	got := instructionContext(t, disabled, nil)
	if got.InstructionFiles.AutoLoad || len(got.InstructionFiles.Files) != 0 {
		t.Fatal("disabled autoload read files")
	}
}

func TestInstructionContextBridgePreservesSharedShape(t *testing.T) {
	rt := newInstructionRuntime(t, nil)
	writeInstructionFixture(t, rt.cfg.AgentDockHome, "bridge-global-marker")
	writeInstructionFixture(t, rt.ws.Root(), "bridge-project-marker")
	result, err := rt.AgentDockLocalContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := result["instruction_files"]; exists {
		t.Fatal("local-only extension leaked into shared Bridge context")
	}
	var got capabilityContext
	if err := remarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	rules := strings.Join(got.Rules, "\n")
	for _, marker := range []string{"bridge-global-marker", "bridge-project-marker"} {
		if strings.Count(rules, marker) != 1 {
			t.Fatalf("Bridge missing or repeated %s", marker)
		}
	}
}

func TestInstructionContextRejectsInvalidWorkdirAndUnknownFields(t *testing.T) {
	rt := newInstructionRuntime(t, nil)
	file := writeInstructionFixture(t, rt.ws.Root(), "rules")
	for _, args := range []map[string]any{
		{"workdir": 42}, {"workdir": nil}, {"workdir": file},
		{"workdir": filepath.Join(rt.ws.Root(), "missing")},
		{"workdir": "bad\x00path"}, {"unknown": true}, {"workdir": strings.Repeat("x", 4097)},
	} {
		_, err := rt.Call(t.Context(), "agentdock_context", args)
		var toolErr *ToolError
		if !errors.As(err, &toolErr) || toolErr.Code != "INVALID_ARGUMENT" {
			t.Fatalf("args=%#v error=%v", args, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := rt.Call(ctx, "agentdock_context", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}
