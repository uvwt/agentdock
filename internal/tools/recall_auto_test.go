package tools

import (
	"context"
	"testing"
)

func TestRecallWriteDeleteRequiresConfirmationLocally(t *testing.T) {
	store := map[string]string{"devices/test.md": "# Test\n"}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	_, err := rt.recallWrite(context.Background(), map[string]any{
		"target": "markdown",
		"action": "delete",
		"path":   "devices/test.md",
	})
	if err == nil {
		t.Fatal("expected delete to require confirmed=true before calling RecallDock")
	}
	if _, ok := store["devices/test.md"]; !ok {
		t.Fatalf("unconfirmed delete must not mutate store")
	}
}

func TestRecallWriteRequiresExplicitTargetAction(t *testing.T) {
	store := map[string]string{}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	_, err := rt.recallWrite(context.Background(), map[string]any{
		"title":   "缺少 target/action",
		"content": "没有显式选择 target/action 时应该返回校验错误，而不是静默走 auto。",
	})
	if err == nil {
		t.Fatal("expected missing target/action to fail")
	}
	if len(store) != 0 {
		t.Fatalf("missing target/action must not write, store=%#v", store)
	}
}

func TestRecallWriteAutoPlanDoesNotWriteAndRecommendsCard(t *testing.T) {
	store := map[string]string{}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	res, err := rt.recallWrite(context.Background(), map[string]any{
		"target":  "auto",
		"action":  "plan",
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
	if got, _ := nextArgs["target"].(string); got != "card" {
		t.Fatalf("next call should use explicit card target, got %#v", nextArgs)
	}
	if got, _ := nextArgs["action"].(string); got != "plan" {
		t.Fatalf("next call should use action=plan, got %#v", nextArgs)
	}
}

func TestRecallWriteAutoPlanRecommendsMarkdownForKnownPathContent(t *testing.T) {
	store := map[string]string{}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	res, err := rt.recallWrite(context.Background(), map[string]any{
		"target":  "auto",
		"action":  "plan",
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
	if got, _ := nextArgs["target"].(string); got != "markdown" {
		t.Fatalf("next call should use explicit markdown target, got %#v", nextArgs)
	}
	if got, _ := nextArgs["action"].(string); got != "write" {
		t.Fatalf("next call should use action=write, got %#v", nextArgs)
	}
	if confirmed, _ := nextArgs["confirmed"].(bool); confirmed {
		t.Fatalf("auto plan must not set confirmed=true: %#v", nextArgs)
	}
}
