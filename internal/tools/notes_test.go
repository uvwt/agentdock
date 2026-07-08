package tools

import (
	"context"
	"strings"
	"testing"
)

func TestNotesSearchAndCapturePlan(t *testing.T) {
	store := map[string]string{
		"recall/managed/notes/questions/index.md":                      "# Index\n- memory-organization.md：记忆、笔记、检索策略\n",
		"recall/managed/notes/questions/topics/memory-organization.md": "# 记忆与笔记组织\n这里讨论 notes 检索、记忆和笔记边界。\n",
	}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	res, err := rt.notesSearch(context.Background(), map[string]any{"scope": "questions", "query": "记忆 笔记 检索"})
	if err != nil {
		t.Fatal(err)
	}
	if action, _ := res["recommended_action"].(string); action != "update_existing" {
		t.Fatalf("expected update_existing, got %#v", res)
	}
	paths := res["candidate_paths"].([]string)
	if len(paths) == 0 || paths[0] != "recall/managed/notes/questions/topics/memory-organization.md" {
		t.Fatalf("unexpected candidate paths: %#v", res)
	}
	if _, ok := res["search_results"]; ok {
		t.Fatalf("recall note search should hide raw search results by default: %#v", res)
	}
	if count, _ := res["search_result_count"].(int); count == 0 {
		t.Fatalf("recall note search should retain search result count: %#v", res)
	}
	res, err = rt.notesSearch(context.Background(), map[string]any{"scope": "questions", "query": "记忆 笔记 检索", "include_search_results": true})
	if err != nil {
		t.Fatal(err)
	}
	if results, ok := res["search_results"].([]any); !ok || len(results) == 0 {
		t.Fatalf("include_search_results should expose raw search results: %#v", res)
	}

	before := store["recall/managed/notes/questions/topics/memory-organization.md"]
	capture, err := rt.notesCapture(context.Background(), map[string]any{"scope": "questions", "question": "文件变多后检索方式要不要改？", "conclusion": "使用 index-first recall_search target=note。", "open_questions": []any{"github-learning 是否也要统一搜索？"}})
	if err != nil {
		t.Fatal(err)
	}
	plan := capture["capture_plan"].(map[string]any)
	if autoWrite, _ := plan["auto_write"].(bool); autoWrite {
		t.Fatalf("recall note capture must not auto-write: %#v", plan)
	}
	draft := plan["draft"].(map[string]any)
	if got, _ := draft["conclusion"].(string); got != "使用 index-first recall_search target=note。" {
		t.Fatalf("note conclusion missing from draft: %#v", draft)
	}
	if openQuestions, ok := draft["open_questions"].([]string); !ok || len(openQuestions) != 1 || openQuestions[0] != "github-learning 是否也要统一搜索？" {
		t.Fatalf("note open_questions missing from draft: %#v", draft)
	}
	if store["recall/managed/notes/questions/topics/memory-organization.md"] != before {
		t.Fatalf("recall note capture wrote content unexpectedly")
	}
}

func TestNotesWriteBoundaries(t *testing.T) {
	store := map[string]string{"recall/managed/notes/questions/index.md": "# Index\n"}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	_, err := rt.notesWrite(context.Background(), map[string]any{"scope": "questions", "path": "recall/docs/projects/agentdock/project.md", "content": "# Bad", "confirmed": true})
	if err == nil {
		t.Fatal("expected recall_write target=note to reject non-notes path")
	}
	_, err = rt.notesWrite(context.Background(), map[string]any{"scope": "questions", "path": "recall/managed/notes/questions/topics/test.md", "content": "# Test"})
	if err == nil {
		t.Fatal("expected recall_write target=note to require confirmation")
	}
	_, err = rt.notesWrite(context.Background(), map[string]any{"scope": "questions", "path": "recall/managed/notes/questions/topics/test.md", "content": "# Test", "confirmed": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(store["recall/managed/notes/questions/topics/test.md"], "# Test") {
		t.Fatalf("expected notes content to be written, got %#v", store)
	}
}

func TestNotesGithubLearningScopeThroughPublicArgsAndPathInference(t *testing.T) {
	store := map[string]string{
		"recall/managed/notes/github-learning/index.md":                "# GitHub Learning Index\n- actions-cache.md：GitHub Actions cache patterns\n",
		"recall/managed/notes/github-learning/topics/actions-cache.md": "# Actions cache\nGitHub Actions cache notes.\n",
	}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	res, err := rt.recallSearch(context.Background(), map[string]any{"kind": "note", "note_scope": "github-learning", "query": "Actions cache"})
	if err != nil {
		t.Fatal(err)
	}
	if scope, _ := res["scope"].(string); scope != "github-learning" {
		t.Fatalf("expected github-learning note search scope, got %#v", res)
	}
	paths := res["candidate_paths"].([]string)
	if len(paths) == 0 || paths[0] != "recall/managed/notes/github-learning/topics/actions-cache.md" {
		t.Fatalf("unexpected github-learning candidate paths: %#v", res)
	}

	capture, err := rt.recallWrite(context.Background(), map[string]any{"target": "note", "action": "plan", "note_scope": "github-learning", "query": "GitHub Actions cache TTL?"})
	if err != nil {
		t.Fatal(err)
	}
	plan := capture["capture_plan"].(map[string]any)
	if target, _ := plan["target_path"].(string); !strings.HasPrefix(target, "recall/managed/notes/github-learning/") {
		t.Fatalf("expected github-learning capture target, got %#v", plan)
	}

	_, err = rt.recallWrite(context.Background(), map[string]any{
		"target":    "note",
		"action":    "create",
		"path":      "recall/managed/notes/github-learning/topics/new-note.md",
		"content":   "# New GitHub note\n",
		"confirmed": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(store["recall/managed/notes/github-learning/topics/new-note.md"], "# New GitHub note") {
		t.Fatalf("expected github-learning note to be written by path inference, got %#v", store)
	}
}
