package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/goal"
)

func TestBookPartGatesAndNextMissing(t *testing.T) {
	dir := t.TempDir()
	parts := filepath.Join(dir, "parts_r9")
	if err := os.MkdirAll(parts, 0o755); err != nil {
		t.Fatal(err)
	}
	p1 := filepath.Join(parts, "letters_01.md")
	p2 := filepath.Join(parts, "letters_02.md")
	if err := os.WriteFile(p1, []byte(strings.Repeat("中", 9000)), 0o644); err != nil {
		t.Fatal(err)
	}
	// letters_02 missing
	g := goal.Goal{
		SuccessCriteria: []goal.SuccessCriterion{
			{ID: "p01_bytes", Expression: "file_min_bytes:" + p1 + ":8000"},
			{ID: "p02_bytes", Expression: "file_min_bytes:" + p2 + ":8000"},
		},
	}
	part, ok := nextMissingPart(g)
	if !ok || part.Path != p2 {
		t.Fatalf("expected missing p2, got ok=%v part=%+v", ok, part)
	}
	// create p2 large enough
	if err := os.WriteFile(p2, []byte(strings.Repeat("文", 9000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := nextMissingPart(g); ok {
		t.Fatal("expected no missing parts")
	}
}

func TestBuildNextBatchRequestUsesSrcBatch(t *testing.T) {
	dir := t.TempDir()
	parts := filepath.Join(dir, "parts_r9")
	src := filepath.Join(parts, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	p1 := filepath.Join(parts, "letters_01.md")
	batch := filepath.Join(src, "batch_01_10.txt")
	if err := os.WriteFile(batch, []byte(strings.Repeat("Letter 1. Hello world. ", 50)), 0o644); err != nil {
		t.Fatal(err)
	}
	g := goal.Goal{
		SuccessCriteria: []goal.SuccessCriterion{
			{ID: "p01_bytes", Expression: "file_min_bytes:" + p1 + ":8000"},
		},
	}
	req, prob, ok := buildNextBatchRequest(g, true)
	if !ok {
		t.Fatal("expected ok")
	}
	if !strings.Contains(req, "停止 thrash") && !strings.Contains(req, "search_text") {
		t.Fatalf("expected thrash language: %s", req)
	}
	if !strings.Contains(req, p1) {
		t.Fatalf("missing part path: %s", req)
	}
	if !strings.Contains(req, batch) {
		t.Fatalf("missing batch path: %s", req)
	}
	if !strings.Contains(prob, "letters_01") {
		t.Fatalf("problem: %s", prob)
	}
}

func TestIsSoftRecoverableBlock(t *testing.T) {
	if !isSoftRecoverableBlock("block: 安全層阻止讀取原文") {
		t.Fatal("expected soft")
	}
	if isSoftRecoverableBlock("orchestrator: tool permission auto-approve failed") {
		t.Fatal("permission must stay hard")
	}
	if isSoftRecoverableBlock("orchestrator: no MCP/tool activity after resume paste") {
		t.Fatal("no mcp must stay hard")
	}
	if !isSoftRecoverableBlock("no_progress for 2 consecutive reasoning turns") {
		t.Fatal("no_progress soft")
	}
}

func TestHasDurableBookProgress(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "letters_01.md")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := goal.Goal{
		SuccessCriteria: []goal.SuccessCriterion{
			{ID: "p01_bytes", Expression: "file_min_bytes:" + p + ":1"},
		},
	}
	if !hasDurableBookProgress(g) {
		t.Fatal("expected durable progress")
	}
	if hasDurableBookProgress(goal.Goal{}) {
		t.Fatal("empty goal should not have progress")
	}
}
