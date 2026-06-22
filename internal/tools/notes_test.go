package tools

import (
	"context"
	"strings"
	"testing"
)

func TestNotesSearchAndCapturePlan(t *testing.T) {
	store := map[string]string{
		"notes/questions/index.md":                      "# Index\n- memory-organization.md：记忆、笔记、检索策略\n",
		"notes/questions/topics/memory-organization.md": "# 记忆与笔记组织\n这里讨论 notes 检索、记忆和笔记边界。\n",
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
	if len(paths) == 0 || paths[0] != "notes/questions/topics/memory-organization.md" {
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

	before := store["notes/questions/topics/memory-organization.md"]
	capture, err := rt.notesCapture(context.Background(), map[string]any{"scope": "questions", "question": "文件变多后检索方式要不要改？", "conclusion": "使用 index-first recall_search kind=note。"})
	if err != nil {
		t.Fatal(err)
	}
	plan := capture["capture_plan"].(map[string]any)
	if autoWrite, _ := plan["auto_write"].(bool); autoWrite {
		t.Fatalf("recall note capture must not auto-write: %#v", plan)
	}
	if store["notes/questions/topics/memory-organization.md"] != before {
		t.Fatalf("recall note capture wrote content unexpectedly")
	}
}

func TestNotesWriteBoundaries(t *testing.T) {
	store := map[string]string{"notes/questions/index.md": "# Index\n"}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	_, err := rt.notesWrite(context.Background(), map[string]any{"scope": "questions", "path": "projects/agentdock/project.md", "content": "# Bad", "confirmed": true})
	if err == nil {
		t.Fatal("expected recall_write kind=note to reject non-notes path")
	}
	_, err = rt.notesWrite(context.Background(), map[string]any{"scope": "questions", "path": "notes/questions/topics/test.md", "content": "# Test"})
	if err == nil {
		t.Fatal("expected recall_write kind=note to require confirmation")
	}
	_, err = rt.notesWrite(context.Background(), map[string]any{"scope": "questions", "path": "notes/questions/topics/test.md", "content": "# Test", "confirmed": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(store["notes/questions/topics/test.md"], "# Test") {
		t.Fatalf("expected notes content to be written, got %#v", store)
	}
}
