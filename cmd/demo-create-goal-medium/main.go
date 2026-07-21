// Command demo-create-goal-medium creates a multi-batch Goal Mode demo:
// 3 progressive letter parts (letters_01..03) from staged English batch files.
// Larger than micro, still far smaller than a full-book job.
//
//	go run ./cmd/demo-create-goal-medium
//	go run ./cmd/demo-create-goal-medium -n 2
//	MEDIUM_RUN_INDEX=3 go run ./cmd/demo-create-goal-medium
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
	runIndex := flag.Int("n", 0, "isolated workspace index → <tmpdir>/agentdock-medium-goal-N (or env MEDIUM_RUN_INDEX)")
	flag.Parse()
	if *runIndex == 0 {
		if v := strings.TrimSpace(os.Getenv("MEDIUM_RUN_INDEX")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				*runIndex = n
			}
		}
		// Fall back to MICRO_RUN_INDEX so the shared harness --run-index works for both.
		if *runIndex == 0 {
			if v := strings.TrimSpace(os.Getenv("MICRO_RUN_INDEX")); v != "" {
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					*runIndex = n
				}
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

	baseName := "agentdock-medium-goal"
	if *runIndex > 0 {
		baseName = fmt.Sprintf("agentdock-medium-goal-%d", *runIndex)
	}
	base := filepath.Join(os.TempDir(), baseName)
	parts := filepath.Join(base, "parts")
	src := filepath.Join(parts, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		fatal(err)
	}

	// 3 English batches × 2 short letters = 6 tiny letters total.
	batches := []struct {
		name string
		body string
	}{
		{
			name: "batch_01.txt",
			body: strings.TrimSpace(`
===== LETTER 1 =====
1. Keep your mind still at dawn and evening. Repeat the holy Name softly
until restlessness settles. A few minutes of sincere remembrance outweigh
hours of restless talk.

===== LETTER 2 =====
2. When worldly duties press, do them carefully, then return to the
inner practice. Do not abandon work; do not abandon the Name either.
`) + "\n",
		},
		{
			name: "batch_02.txt",
			body: strings.TrimSpace(`
===== LETTER 3 =====
3. Trust that livelihood comes as needed. Anxiety about tomorrow only
steals today's peace. Serve honestly and leave the fruit to the Lord.

===== LETTER 4 =====
4. Company of the Master, even in memory, is true wealth. Prefer one
hour of quiet listening over many hours of empty argument.
`) + "\n",
		},
		{
			name: "batch_03.txt",
			body: strings.TrimSpace(`
===== LETTER 5 =====
5. Listen daily to the Sound Current within. Begin gently; consistency
matters more than force. Write a short note of gratitude after practice.

===== LETTER 6 =====
6. Love softens the heart so the teaching can enter. Keep humility,
avoid display, and let service be quiet and steady.
`) + "\n",
		},
	}

	batchPaths := make([]string, 0, len(batches))
	for _, b := range batches {
		p := filepath.Join(src, b.name)
		if err := os.WriteFile(p, []byte(b.body), 0o644); err != nil {
			fatal(err)
		}
		batchPaths = append(batchPaths, p)
	}

	final := filepath.Join(base, "medium_letters_zh.md")
	part1 := filepath.Join(parts, "letters_01.md")
	part2 := filepath.Join(parts, "letters_02.md")
	part3 := filepath.Join(parts, "letters_03.md")

	workspaceID := "medium-goal-3batch"
	if *runIndex > 0 {
		workspaceID = fmt.Sprintf("medium-goal-3batch-%d", *runIndex)
	}

	in := goal.CreateInput{
		Title:       "Medium Goal Mode demo — 3-batch multi-part ZH (not full book)",
		Objective:   "Medium multi-batch Goal Mode demo: translate 3 staged English letter batches (6 tiny letters) to Traditional Chinese via ChatGPT web + AgentDock MCP only. Progressive parts letters_01..03 then assemble final. Workspace parts at " + parts,
		Mode:        goal.ModeAutopilot,
		WorkspaceID: workspaceID,
		Budget: &goal.Budget{
			MaxReasoningTurns:        30,
			MaxReplans:               5,
			MaxConversationRotations: 5,
			MaxRuntimeMinutes:        120,
			MaxIdenticalFailures:     3,
			MaxBrowserRetries:        3,
		},
	}
	// PartMinBytes ~300–500 so micro-sized batches can pass without huge translation.
	// FinalMinBytes ~1200, FinalMinLines ~10 for assembled 3 parts.
	goal.ApplyBookJobTemplate(&in, goal.BookJobTemplateInput{
		Kind:          goal.BookJobLetter,
		PartsDir:      parts,
		OutputPath:    final,
		PartCount:     3,
		PartMinBytes:  350,
		FinalMinBytes: 1200,
		FinalMinLines: 10,
	})

	g, err := store.Create(in)
	if err != nil {
		fatal(err)
	}

	req := fmt.Sprintf(
		"【中型 multi-batch demo — 禁止 decision=block · 禁止 search_text】\n"+
			"來源批次（請依序處理）：\n"+
			"1) read_file %s → file_edit atomic_write %s （第1–2封繁中，#標題）→ commit_turn continue\n"+
			"2) read_file %s → file_edit atomic_write %s （第3–4封繁中，#標題）→ commit_turn continue\n"+
			"3) read_file %s → file_edit atomic_write %s （第5–6封繁中，#標題）→ commit_turn continue\n"+
			"4) 合併 parts 到 %s（atomic_write）→ verify → commit_turn complete\n"+
			"流程：get → acquire_lease(chatgpt-model) → 每完成一段立刻 commit_turn。\n"+
			"禁止 search_text。禁止 decision=block。必須 progressive 寫 parts 再組裝 final。\n",
		batchPaths[0], part1,
		batchPaths[1], part2,
		batchPaths[2], part3,
		final,
	)
	g, err = store.RequestReasoning(g.ID, req, "medium demo: 3 batches → progressive parts → assemble final")
	if err != nil {
		fatal(err)
	}

	fmt.Printf("goal_id=%s\n", g.ID)
	fmt.Printf("status=%s\n", g.Status)
	fmt.Printf("workspace=%s\n", g.WorkspaceID)
	fmt.Printf("workspace_base=%s\n", base)
	fmt.Printf("parts_dir=%s\n", parts)
	fmt.Printf("parts_path=%s\n", parts)
	fmt.Printf("parts_01=%s\n", part1)
	fmt.Printf("parts_02=%s\n", part2)
	fmt.Printf("parts_03=%s\n", part3)
	fmt.Printf("src_batch_01=%s\n", batchPaths[0])
	fmt.Printf("src_batch_02=%s\n", batchPaths[1])
	fmt.Printf("src_batch_03=%s\n", batchPaths[2])
	fmt.Printf("final_path=%s\n", final)
	fmt.Printf("run_index=%d\n", *runIndex)
	fmt.Printf("part_count=3\n")
	fmt.Printf("created_at=%s\n", g.CreatedAt.Format(time.RFC3339))
	fmt.Println("next: open ChatGPT worker, optional auto_approve, orchestrate_start (do NOT hard-reset Chrome by default)")
	fmt.Println("success = completed progressive 3-part medium demo without full-book scale")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
