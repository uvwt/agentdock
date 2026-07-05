package tools

import (
	"context"
	"strings"
	"testing"
)

func TestMemoryCardCapturePlansWithoutWriting(t *testing.T) {
	store := map[string]string{
		"recall/managed/cards/chatdock/active/project_trap/deploy-check.md": "---\ntype: recall-card\nproject: chatdock\n---\n\n# Deploy Check\nChatDock deploy verification needs public smoke check.\n",
	}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	before := len(store)
	res, err := rt.memoryCardCapture(context.Background(), map[string]any{
		"title":   "ChatDock 部署验证",
		"content": "ChatDock 前端嵌入 Go 二进制后，部署验证必须检查最终服务页面，而不是只看源码或 dist 文件。",
		"type":    "project_trap",
		"project": "chatdock",
		"tags":    []any{"deploy", "frontend"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(store) != before {
		t.Fatalf("card capture must not write, store=%#v", store)
	}
	plan := res["capture_plan"].(Result)
	if autoWrite, _ := plan["auto_write"].(bool); autoWrite {
		t.Fatalf("capture plan should never auto-write: %#v", plan)
	}
	if similar, _ := res["similar_count"].(int); similar == 0 {
		t.Fatalf("expected similar card detection, got %#v", res)
	}
}

func TestMemoryCardWriteRequiresConfirmationAndUsesCardsPath(t *testing.T) {
	store := map[string]string{}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	args := map[string]any{
		"title":      "RSS Monitor 事实层现场检查",
		"content":    "RSS Monitor 排障时，历史经验只能作为提醒；当前 compose、配置、数据库和运行结果必须现场检查后再判断。",
		"type":       "runbook",
		"scope":      "project",
		"project":    "rss-monitor",
		"status":     "inbox",
		"confidence": "high",
		"tags":       []any{"debugging", "recall"},
	}
	_, err := rt.memoryCardWrite(context.Background(), args)
	if err == nil {
		t.Fatal("expected card write to require confirmation")
	}
	args["confirmed"] = true
	res, err := rt.memoryCardWrite(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := res["path"].(string)
	if !strings.HasPrefix(p, "recall/managed/cards/rss-monitor/inbox/runbook/") {
		t.Fatalf("unexpected card path %q", p)
	}
	content := store[recallBackendPath(p)]
	for _, want := range []string{"type: recall-card", "card_type: runbook", "project: rss-monitor", "status: inbox", "# RSS Monitor 事实层现场检查"} {
		if !strings.Contains(content, want) {
			t.Fatalf("written card missing %q: %s", want, content)
		}
	}
}

func TestMemoryCardDefaultsGlobalScopeWhenProjectOmitted(t *testing.T) {
	card, _, err := memoryCardFromArgs(map[string]any{
		"title":   "通用偏好",
		"content": "用户偏好直接执行可自动完成的操作，不要让用户代替完成工具可执行的步骤。",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if card.Scope != "global" || card.Project != "global" {
		t.Fatalf("project omitted should default to global/global, got scope=%q project=%q", card.Scope, card.Project)
	}
}

func TestMemoryCardDefaultsProjectScopeWhenProjectExplicit(t *testing.T) {
	card, _, err := memoryCardFromArgs(map[string]any{
		"title":   "ChatDock 部署目录",
		"content": "ChatDock 部署时必须优先检查专用 compose 目录，不能误用默认仓库目录。",
		"project": "chatdock",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if card.Scope != "project" || card.Project != "chatdock" {
		t.Fatalf("explicit project should default to project scope, got scope=%q project=%q", card.Scope, card.Project)
	}
}

func TestMemoryCardWriteBlocksReviewedWarningsByDefault(t *testing.T) {
	store := map[string]string{}
	rt, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()

	_, err := rt.memoryCardWrite(context.Background(), map[string]any{
		"title":     "临时状态",
		"content":   "当前端口是 1234。",
		"project":   "demo",
		"confirmed": true,
	})
	if err == nil {
		t.Fatal("expected warning content to require explicit allow_warnings")
	}
}
