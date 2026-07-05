package tools

import (
	"context"
	"testing"
)

func TestRecallWriteRequiresExplicitKind(t *testing.T) {
	store := map[string]string{}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	_, err := rt.recallWrite(context.Background(), map[string]any{
		"title":   "缺少 kind",
		"content": "没有显式选择记忆形态时应该返回校验错误，而不是静默走 auto。",
	})
	if err == nil {
		t.Fatal("expected missing kind to fail")
	}
	if len(store) != 0 {
		t.Fatalf("missing kind must not write, store=%#v", store)
	}
}

func TestRecallWriteAutoPlanDoesNotWriteAndRecommendsCard(t *testing.T) {
	store := map[string]string{}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	res, err := rt.recallWrite(context.Background(), map[string]any{
		"kind":    "auto",
		"title":   "直接执行偏好",
		"content": "用户偏好直接执行可以自动完成的操作，不要反复确认或让用户代替完成。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(store) != 0 {
		t.Fatalf("auto plan must not write, store=%#v", store)
	}
	if got, _ := res["selected_kind"].(string); got != "card" {
		t.Fatalf("expected auto plan to recommend card, got %#v", res)
	}
	plan := res["auto_plan"].(Result)
	if autoWrite, _ := plan["auto_write"].(bool); autoWrite {
		t.Fatalf("auto plan should never auto-write: %#v", plan)
	}
	nextCall := plan["next_call"].(Result)
	nextArgs := nextCall["args"].(Result)
	if got, _ := nextArgs["kind"].(string); got != "card" {
		t.Fatalf("next call should use explicit card kind, got %#v", nextArgs)
	}
}

func TestRecallWriteAutoPlanRecommendsMarkdownForKnownPathContent(t *testing.T) {
	store := map[string]string{}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	res, err := rt.recallWrite(context.Background(), map[string]any{
		"kind":    "auto",
		"path":    "projects/demo/project.md",
		"content": "# Demo\n稳定项目文档。\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(store) != 0 {
		t.Fatalf("auto plan must not write, store=%#v", store)
	}
	if got, _ := res["selected_kind"].(string); got != "markdown" {
		t.Fatalf("expected auto plan to recommend markdown, got %#v", res)
	}
	plan := res["auto_plan"].(Result)
	nextCall := plan["next_call"].(Result)
	nextArgs := nextCall["args"].(Result)
	if got, _ := nextArgs["kind"].(string); got != "markdown" {
		t.Fatalf("next call should use explicit markdown kind, got %#v", nextArgs)
	}
	if confirmed, _ := nextArgs["confirmed"].(bool); confirmed {
		t.Fatalf("auto plan must not set confirmed=true: %#v", nextArgs)
	}
}
