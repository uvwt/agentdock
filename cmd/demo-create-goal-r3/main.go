package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/goal"
)

func main() {
	home, _ := os.UserHomeDir()
	store, err := goal.New(filepath.Join(home, ".agentdock", "goals"))
	if err != nil {
		panic(err)
	}

	for _, id := range []string{
		"goal_98c4860260a6741c", // r8
		"goal_8d769664513fe2d6", // r7
		"goal_ca32391f3f73087e", // r6
		"goal_1b29548f18efeb8b", // r5
		"goal_4c0d2135cc72d575", // r4
	} {
		if g, err := store.Get(id); err == nil {
			switch g.Status {
			case goal.StatusCancelled, goal.StatusCompleted, goal.StatusFailed:
			default:
				if _, err := store.Cancel(g.ID, "superseded by r9 after post-paste allow-stop CDP reduce"); err != nil {
					fmt.Fprintf(os.Stderr, "cancel %s: %v\n", id, err)
				} else {
					fmt.Fprintln(os.Stderr, "cancelled", id)
				}
			}
		}
	}

	pdf := "/Users/sigi/Documents/真理書密室/英文/RSSB/《靈性書信》Spiritual-Letters-Jaimal Singh.pdf"
	out := "/Users/sigi/Documents/真理書密室/英文/RSSB/《靈性書信》Spiritual-Letters-Jaimal Singh_繁體中文_goaltest_r9.md"
	parts := filepath.Join(filepath.Dir(out), "parts_r9")
	_ = os.MkdirAll(parts, 0o755)

	title := "《靈性書信》繁中完整翻譯（Goal 重測 r9 / allow-stop hands-off after first approve）"
	objective := strings.Join([]string{
		"將 PDF " + pdf + " 完整翻譯為自然忠實繁體中文，並輸出 Markdown 到 " + out + "。",
		"必須完整書信主體，不可只寫前言。",
		"完成標準至少 file_min_bytes>=80000、有 # 標題、不得含「目前已完成前言/後續章節將/待續」。",
		"使用 ChatGPT 網頁 + AgentDock MCP；長文用 file_edit action=atomic_write；腳本先 file_edit 再建 run_command。",
		"本機已備 /tmp/spiritual_letters_goal/full_raw.txt 與 inspect.py。",
		"分段產物請寫到 " + parts + "（letters_01.md …），最後再組裝最終檔。",
		"第一個 commit 必須至少產出 parts_r9/letters_01.md 或可驗證中間產物。",
	}, "")

	createIn := goal.CreateInput{
		Title:       title,
		Objective:   objective,
		WorkspaceID: "spiritual-letters-goal-r9",
		Mode:        goal.ModeAutopilot,
		SuccessCriteria: []goal.SuccessCriterionInput{
			{ID: "c_manual_quality", Type: goal.CriterionManual, Expression: "全書書信主體已翻譯，非僅序言；人名與靈修術語一致"},
		},
		Constraints: []goal.Constraint{
			{Type: goal.ConstraintProhibition, Value: "禁止請本機 Claude 代寫正文"},
			{Type: goal.ConstraintProhibition, Value: "不得修改原始 PDF: " + pdf},
			{Type: goal.ConstraintQuality, Value: "只用 ChatGPT 網頁 + AgentDock MCP"},
		},
		Budget: &goal.Budget{
			MaxReasoningTurns:        40,
			MaxReplans:               8,
			MaxConversationRotations: 8,
			MaxRuntimeMinutes:        240,
			MaxIdenticalFailures:     3,
		},
	}
	if tmpl, ok := goal.SuggestBookJobFromObjective(createIn.Title, createIn.Objective, out); ok {
		tmpl.OutputPath = out
		tmpl.SourcePDF = pdf
		tmpl.PartsDir = parts
		tmpl.FinalMinBytes = 80000
		tmpl.PartMinBytes = 8000
		goal.ApplyBookJobTemplate(&createIn, tmpl)
	}
	g, err := store.Create(createIn)
	if err != nil {
		panic(err)
	}
	req := strings.Join([]string{
		"【Goal Mode 完整翻譯重測 r9 — allow-stop after first auto-approve; no post-allow CDP storm】",
		"PDF: " + pdf,
		"輸出: " + out,
		"parts: " + parts,
		"硬性門檻: >= 80000 bytes；不得只寫前言；不得「待續/後續章節將/目前已完成前言」。",
		"本機已備 /tmp/spiritual_letters_goal/full_raw.txt 與 inspect.py。",
		"必須：",
		"1) 先 goal_manage get",
		"2) python3 /tmp/spiritual_letters_goal/inspect.py",
		"3) 分批翻譯，每批 file_edit atomic_write 到 " + parts + "/letters_XX.md",
		"4) 組裝最終檔 atomic_write；驗證後 commit_turn",
		"若 SCRIPT_MISSING：先 file_edit 建腳本再 run_command。只用 ChatGPT 網頁+MCP。",
		"第一個 commit 必須至少產出 letters_01.md。若出現 Svananda 權限窗請允許後立刻繼續工具。",
	}, "\n")
	g, err = store.RequestReasoning(g.ID, req, "Fresh r9 after post-paste allow-stop (35s max, return on first auto_approved). Full Traditional Chinese book required; do not only chat.")
	if err != nil {
		panic(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"goal_id":         g.ID,
		"status":          g.Status,
		"capsule_version": g.CapsuleVersion,
		"milestones":      len(g.Milestones),
		"criteria":        len(g.SuccessCriteria),
		"workspace_id":    g.WorkspaceID,
		"output":          out,
		"parts_dir":       parts,
	})
	for _, m := range g.Milestones {
		fmt.Fprintf(os.Stderr, "milestone %s %s\n", m.ID, m.Title)
	}
}
