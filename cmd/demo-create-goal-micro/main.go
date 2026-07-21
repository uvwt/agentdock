// Command demo-create-goal-micro creates a short Goal Mode acceptance demo:
// 3-letter micro book job with progressive criteria — proves loop without 80KB torture.
//
//	go run ./cmd/demo-create-goal-micro
//	go run ./cmd/demo-create-goal-micro -n 2
//	MICRO_RUN_INDEX=3 go run ./cmd/demo-create-goal-micro
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/goal"
)

func main() {
	runIndex := flag.Int("n", 0, "isolated workspace index → <tmpdir>/agentdock-micro-goal-N (or env MICRO_RUN_INDEX)")
	flag.Parse()
	if *runIndex == 0 {
		if v := strings.TrimSpace(os.Getenv("MICRO_RUN_INDEX")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				*runIndex = n
			}
		}
	}

	root := os.Getenv("AGENTDOCK_GOALS_DIR")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".agentdock", "goals")
	}
	store, err := goal.New(root)
	if err != nil {
		fatal(err)
	}

	baseName := "agentdock-micro-goal"
	if *runIndex > 0 {
		baseName = fmt.Sprintf("agentdock-micro-goal-%d", *runIndex)
	}
	base := filepath.Join(os.TempDir(), baseName)
	parts := filepath.Join(base, "parts")
	src := filepath.Join(parts, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		fatal(err)
	}

	batch := filepath.Join(src, "batch_01_03.txt")
	body := strings.TrimSpace(`
===== LETTER 1 =====
1. Keep your mind still and remember the Name each day.

===== LETTER 2 =====
2. Do not worry about livelihood; the Lord provides what is needed.

===== LETTER 3 =====
3. Love for the Master is true wealth; listen to the Sound Current daily.
`) + "\n"
	if err := os.WriteFile(batch, []byte(body), 0o644); err != nil {
		fatal(err)
	}

	final := filepath.Join(base, "micro_letters_zh.md")
	part1 := filepath.Join(parts, "letters_01.md")
	workspaceID := "micro-goal-3letters"
	if *runIndex > 0 {
		workspaceID = fmt.Sprintf("micro-goal-3letters-%d", *runIndex)
	}
	in := goal.CreateInput{
		Title:       "Micro Goal Mode demo — 3 letters ZH (loop green)",
		Objective:   "Translate 3 short English letters to Traditional Chinese via ChatGPT web + AgentDock MCP only. Prove create→wake→write→commit→verify→complete without operator capsule surgery. parts at " + parts,
		Mode:        goal.ModeAutopilot,
		WorkspaceID: workspaceID,
		Budget: &goal.Budget{
			MaxReasoningTurns:        12,
			MaxReplans:               3,
			MaxConversationRotations: 3,
			MaxRuntimeMinutes:        30,
			MaxIdenticalFailures:     3,
			MaxBrowserRetries:        3,
		},
	}
	goal.ApplyBookJobTemplate(&in, goal.BookJobTemplateInput{
		Kind:          goal.BookJobLetter,
		PartsDir:      parts,
		OutputPath:    final,
		PartCount:     1,
		PartMinBytes:  400,
		FinalMinBytes: 500,
		FinalMinLines: 5,
	})

	g, err := store.Create(in)
	if err != nil {
		fatal(err)
	}
	req := fmt.Sprintf(
		"【短 demo — 禁止 decision=block】\nread_file %s\n→ file_edit atomic_write %s （第1–3封繁中，#標題）\n→ 合併到 %s\n→ get → acquire_lease(chatgpt-model) → commit_turn continue/verify\n禁止 search_text。\n",
		batch, part1, final,
	)
	g, err = store.RequestReasoning(g.ID, req, "micro demo: translate 3 letters then assemble")
	if err != nil {
		fatal(err)
	}

	fmt.Printf("goal_id=%s\n", g.ID)
	fmt.Printf("status=%s\n", g.Status)
	fmt.Printf("workspace=%s\n", g.WorkspaceID)
	fmt.Printf("workspace_base=%s\n", base)
	fmt.Printf("parts_dir=%s\n", parts)
	fmt.Printf("parts_path=%s\n", part1)
	fmt.Printf("src_batch=%s\n", batch)
	fmt.Printf("final_path=%s\n", final)
	fmt.Printf("run_index=%d\n", *runIndex)
	fmt.Printf("created_at=%s\n", g.CreatedAt.Format(time.RFC3339))
	fmt.Println("next: open ChatGPT worker, optional auto_approve, orchestrate_start (do NOT hard-reset Chrome by default)")
	fmt.Println("success = completed unattended with zero mid-run code and zero manual capsule surgery")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
