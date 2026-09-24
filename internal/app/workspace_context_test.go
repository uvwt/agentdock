package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/uvwt/agentdock/internal/agentinstructions"
	"github.com/uvwt/agentdock/internal/config"
)

func newWorkspaceContextRuntime(t *testing.T) (*Runtime, string) {
	t.Helper()
	home := t.TempDir()
	setUserHomeForTest(t, home)
	cfg := config.Config{
		AgentDockHome:       filepath.Join(home, "agentdock-state"),
		AgentDockDefaultDir: filepath.Join(home, "default-workspace"),
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
	return rt, home
}

func writeWorkspaceInstruction(t *testing.T, dir, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, agentinstructions.Filename)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func callWorkspaceContext(t *testing.T, rt *Runtime, args map[string]any) workspaceContextResult {
	t.Helper()
	result, err := rt.Call(t.Context(), "workspace_context", args)
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchestestOutputSchema(t, "workspace_context", result)
	var got workspaceContextResult
	if err := remarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func loadedWorkspaceInstructions(value workspaceContextResult) string {
	contents := make([]string, 0, len(value.Instructions))
	for _, file := range value.Instructions {
		if file.Status == "loaded" {
			contents = append(contents, file.Content)
		}
	}
	return strings.Join(contents, "|")
}

func TestWorkspaceContextLoadsFixedGlobalNestedRulesAndLocalSkills(t *testing.T) {
	rt, home := newWorkspaceContextRuntime(t)
	root := rt.ws.Root()
	child := filepath.Join(root, "service", "internal")
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceInstruction(t, filepath.Join(home, ".agentdock"), "fixed-global")
	writeWorkspaceInstruction(t, rt.cfg.AgentDockHome, "configured-home-must-not-load")
	writeWorkspaceInstruction(t, root, "workspace-root")
	writeWorkspaceInstruction(t, filepath.Dir(child), "workspace-service")
	writeWorkspaceInstruction(t, child, "workspace-child")
	writeCommonSkillForTest(t, filepath.Join(root, ".agents", "skills"), "z-skill", "z-skill", "Z workspace skill")
	writeCommonSkillForTest(t, filepath.Join(root, ".agents", "skills"), "a-skill", "a-skill", "A workspace skill")
	writeCommonSkillFileForTest(t, filepath.Join(root, ".agents", "skills", "bad", "SKILL.md"), "not frontmatter")

	got := callWorkspaceContext(t, rt, map[string]any{"workdir": child})
	if got.Workdir != child || got.WorkspaceRoot != root {
		t.Fatalf("workspace selection = %#v", got)
	}
	if body := loadedWorkspaceInstructions(got); body != "fixed-global|workspace-root|workspace-service|workspace-child" {
		t.Fatalf("instruction order/body = %q", body)
	}
	if len(got.WorkspaceSkills) != 2 || got.WorkspaceSkills[0].Name != "a-skill" || got.WorkspaceSkills[1].Name != "z-skill" {
		t.Fatalf("workspace Skill index = %#v", got.WorkspaceSkills)
	}
	if !strings.HasPrefix(got.WorkspaceSkills[0].File, "skill://workspace/") ||
		!strings.HasSuffix(got.WorkspaceSkills[0].File, "/a-skill/SKILL.md") ||
		got.WorkspaceSkills[0].SkillRef == "" || got.WorkspaceSkills[0].SourceType != "workspace" {
		t.Fatalf("workspace Skill file = %q", got.WorkspaceSkills[0].File)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "Follow this workflow") || strings.Contains(string(encoded), "configured-home-must-not-load") {
		t.Fatalf("workspace context leaked Skill body or configured AgentDockHome rule: %s", encoded)
	}
}

func TestWorkspaceContextSwitchingIsFreshAndDoesNotChangeDefaultCWD(t *testing.T) {
	rt, home := newWorkspaceContextRuntime(t)
	writeWorkspaceInstruction(t, filepath.Join(home, ".agentdock"), "global")
	defaultCWD := rt.ws.DefaultCWD()
	projects := []string{t.TempDir(), t.TempDir()}
	for index, project := range projects {
		if err := os.Mkdir(filepath.Join(project, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		writeWorkspaceInstruction(t, project, fmt.Sprintf("project-%d", index))
	}

	first := callWorkspaceContext(t, rt, map[string]any{"workdir": projects[0]})
	second := callWorkspaceContext(t, rt, map[string]any{"workdir": projects[1]})
	if loadedWorkspaceInstructions(first) != "global|project-0" || loadedWorkspaceInstructions(second) != "global|project-1" {
		t.Fatalf("workspace switch leaked context: first=%q second=%q", loadedWorkspaceInstructions(first), loadedWorkspaceInstructions(second))
	}
	writeWorkspaceInstruction(t, projects[0], "project-X")
	refreshed := callWorkspaceContext(t, rt, map[string]any{"workdir": projects[0]})
	if loadedWorkspaceInstructions(refreshed) != "global|project-X" {
		t.Fatalf("workspace refresh returned stale content: %q", loadedWorkspaceInstructions(refreshed))
	}
	if err := os.Remove(filepath.Join(projects[1], agentinstructions.Filename)); err != nil {
		t.Fatal(err)
	}
	deleted := callWorkspaceContext(t, rt, map[string]any{"workdir": projects[1]})
	if len(deleted.Instructions) != 2 || deleted.Instructions[1].Status != "not_found" {
		t.Fatalf("deleted AGENTS.md remained loaded: %#v", deleted.Instructions)
	}
	if rt.ws.DefaultCWD() != defaultCWD {
		t.Fatalf("workspace_context changed default cwd: got=%q want=%q", rt.ws.DefaultCWD(), defaultCWD)
	}
}

func TestWorkspaceContextConcurrentSelectionsRemainIsolated(t *testing.T) {
	rt, home := newWorkspaceContextRuntime(t)
	writeWorkspaceInstruction(t, filepath.Join(home, ".agentdock"), "global")
	projects := []string{t.TempDir(), t.TempDir()}
	for index, project := range projects {
		if err := os.Mkdir(filepath.Join(project, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		writeWorkspaceInstruction(t, project, fmt.Sprintf("project-%d", index))
	}
	defaultCWD := rt.ws.DefaultCWD()

	var wg sync.WaitGroup
	for index := range 16 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			result, err := rt.Call(t.Context(), "workspace_context", map[string]any{"workdir": projects[index%2]})
			if err != nil {
				t.Error(err)
				return
			}
			var got workspaceContextResult
			if err := remarshal(result, &got); err != nil {
				t.Error(err)
				return
			}
			want := fmt.Sprintf("global|project-%d", index%2)
			if body := loadedWorkspaceInstructions(got); body != want {
				t.Errorf("workspace context leaked across requests: got=%q want=%q", body, want)
			}
		}(index)
	}
	wg.Wait()
	if rt.ws.DefaultCWD() != defaultCWD {
		t.Fatal("concurrent workspace selection changed default cwd")
	}
}

func TestWorkspaceContextReportsBoundedInvalidAndOversizedInstructions(t *testing.T) {
	rt, home := newWorkspaceContextRuntime(t)
	global := filepath.Join(home, ".agentdock")
	if err := os.MkdirAll(global, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(global, agentinstructions.Filename), []byte{0xff, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}
	writeWorkspaceInstruction(t, rt.ws.Root(), strings.Repeat("x", agentinstructions.MaxFileBytes+1))

	got := callWorkspaceContext(t, rt, nil)
	if len(got.Instructions) != 2 || got.Instructions[0].Reason != "invalid_utf8_text" || got.Instructions[1].Reason != "file_size_limit" {
		t.Fatalf("invalid instruction status = %#v", got.Instructions)
	}
	for _, file := range got.Instructions {
		if file.Content != "" {
			t.Fatalf("invalid instruction returned partial body: %#v", file)
		}
	}
}

func TestWorkspaceContextSkillIndexTruncatesWithWarning(t *testing.T) {
	rt, _ := newWorkspaceContextRuntime(t)
	root := filepath.Join(rt.ws.Root(), ".agents", "skills")
	for index := 0; index < filesystemSkillIndexLimit+2; index++ {
		name := fmt.Sprintf("skill-%02d", index)
		writeCommonSkillForTest(t, root, name, name, "workspace skill")
	}
	got := callWorkspaceContext(t, rt, nil)
	if len(got.WorkspaceSkills) != filesystemSkillIndexLimit || len(got.Warnings) != 1 || got.Warnings[0].Source != "workspace_skills" {
		t.Fatalf("truncated workspace Skill index = %#v warnings=%#v", got.WorkspaceSkills, got.Warnings)
	}
}

func TestWorkspaceContextRejectsInvalidInputAndHonorsCancellation(t *testing.T) {
	rt, _ := newWorkspaceContextRuntime(t)
	file := writeWorkspaceInstruction(t, rt.ws.Root(), "rules")
	for _, args := range []map[string]any{
		{"workdir": 42}, {"workdir": nil}, {"workdir": file},
		{"workdir": filepath.Join(rt.ws.Root(), "missing")}, {"unknown": true},
	} {
		_, err := rt.Call(t.Context(), "workspace_context", args)
		var toolErr *ToolError
		if !errors.As(err, &toolErr) || toolErr.Code != "INVALID_ARGUMENT" {
			t.Fatalf("args=%#v error=%v", args, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := rt.Call(ctx, "workspace_context", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}

func TestWorkspaceContextDoesNotFollowWorkspaceSkillPackageSymlink(t *testing.T) {
	rt, _ := newWorkspaceContextRuntime(t)
	root := filepath.Join(rt.ws.Root(), ".agents", "skills")
	targetRoot := t.TempDir()
	writeCommonSkillForTest(t, targetRoot, "outside-skill", "outside-skill", "Outside workspace skill.")
	target := filepath.Join(targetRoot, "outside-skill")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got := callWorkspaceContext(t, rt, nil)
	if len(got.WorkspaceSkills) != 0 {
		t.Fatalf("workspace Skill index followed package symlink outside workspace: %#v", got.WorkspaceSkills)
	}
}

func TestWorkspaceContextDoesNotFollowWorkspaceSkillDocumentSymlink(t *testing.T) {
	rt, _ := newWorkspaceContextRuntime(t)
	packageDir := filepath.Join(rt.ws.Root(), ".agents", "skills", "linked-skill")
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "SKILL.md")
	writeCommonSkillFileForTest(t, outside, "---\nname: outside-skill\ndescription: Outside workspace skill.\n---\n\n# Outside\n")
	if err := os.Symlink(outside, filepath.Join(packageDir, "SKILL.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got := callWorkspaceContext(t, rt, nil)
	if len(got.WorkspaceSkills) != 0 {
		t.Fatalf("workspace Skill index followed SKILL.md symlink outside workspace: %#v", got.WorkspaceSkills)
	}
}

func TestWorkspaceContextDoesNotFollowAgentsDirectoryLinkOutsideWorkspace(t *testing.T) {
	rt, _ := newWorkspaceContextRuntime(t)
	outsideAgents := filepath.Join(t.TempDir(), ".agents")
	writeCommonSkillForTest(t, filepath.Join(outsideAgents, "skills"), "outside", "outside-skill", "Outside workspace skill.")
	createWorkspaceDirectoryLinkForTest(t, outsideAgents, filepath.Join(rt.ws.Root(), ".agents"))

	got := callWorkspaceContext(t, rt, nil)
	if len(got.WorkspaceSkills) != 0 {
		t.Fatalf("workspace Skill index escaped through .agents directory link: %#v", got.WorkspaceSkills)
	}
}

func TestWorkspaceContextDoesNotFollowSkillsDirectoryLinkOutsideWorkspace(t *testing.T) {
	rt, _ := newWorkspaceContextRuntime(t)
	outsideSkills := t.TempDir()
	writeCommonSkillForTest(t, outsideSkills, "outside", "outside-skill", "Outside workspace skill.")
	if err := os.MkdirAll(filepath.Join(rt.ws.Root(), ".agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	createWorkspaceDirectoryLinkForTest(t, outsideSkills, filepath.Join(rt.ws.Root(), ".agents", "skills"))

	got := callWorkspaceContext(t, rt, nil)
	if len(got.WorkspaceSkills) != 0 {
		t.Fatalf("workspace Skill index escaped through .agents/skills directory link: %#v", got.WorkspaceSkills)
	}
}
