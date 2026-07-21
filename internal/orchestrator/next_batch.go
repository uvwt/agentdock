package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/uvwt/agentdock/internal/goal"
)

// partGate is a progressive book part file gate from success criteria.
type partGate struct {
	ID       string
	Path     string
	MinBytes int64
}

// bookPartGates extracts ordered file_min_bytes gates for letters_*/chapter_* parts.
func bookPartGates(g goal.Goal) []partGate {
	var out []partGate
	for _, c := range g.SuccessCriteria {
		expr := strings.TrimSpace(c.Expression)
		if !strings.HasPrefix(expr, "file_min_bytes:") {
			continue
		}
		rest := strings.TrimPrefix(expr, "file_min_bytes:")
		i := strings.LastIndex(rest, ":")
		if i <= 0 {
			continue
		}
		path := rest[:i]
		n, err := strconv.ParseInt(rest[i+1:], 10, 64)
		if err != nil || n <= 0 {
			continue
		}
		base := filepath.Base(path)
		if !(strings.HasPrefix(base, "letters_") || strings.HasPrefix(base, "chapter_")) {
			continue
		}
		if !strings.HasSuffix(base, ".md") {
			continue
		}
		out = append(out, partGate{ID: c.ID, Path: path, MinBytes: n})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Path < out[j].Path
	})
	return out
}

// nextMissingPart returns the first part gate whose file is missing or below min bytes.
func nextMissingPart(g goal.Goal) (partGate, bool) {
	for _, p := range bookPartGates(g) {
		st, err := os.Stat(p.Path)
		if err != nil || st.IsDir() || st.Size() < p.MinBytes {
			return p, true
		}
	}
	return partGate{}, false
}

// findSrcBatchNearPart looks for a staged English batch next to the part file.
// Prefer partsDir/src/batch_*.txt, then /tmp/spiritual_letters_goal/src_batches/.
func findSrcBatchNearPart(partPath string) string {
	partsDir := filepath.Dir(partPath)
	candidates := []string{
		filepath.Join(partsDir, "src"),
		filepath.Join(filepath.Dir(partsDir), "src"),
		"/tmp/spiritual_letters_goal/src_batches",
	}
	// Map letters_03 → prefer batch files that look related; else first readable batch
	// larger than empty, sorted by name.
	var found []string
	for _, dir := range candidates {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasPrefix(name, "batch_") || !strings.HasSuffix(name, ".txt") {
				continue
			}
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err == nil && st.Size() > 100 {
				found = append(found, p)
			}
		}
		if len(found) > 0 {
			break
		}
	}
	if len(found) == 0 {
		return ""
	}
	sort.Strings(found)
	// Heuristic: letters_NN.md → prefer batch whose name sorts near remaining work.
	// If part index known, pick batch at index-1 among remaining; else first.
	base := filepath.Base(partPath)
	// letters_03.md → index 3 → often batch after 01/02 done; pick by position among gates is caller's job.
	// Prefer batch that is not already "done" by matching letters_0N to batch order.
	idx := partIndex(base)
	if idx > 0 && idx-1 < len(found) {
		// parts 1..N map roughly to batch order; use idx-1 capped
		i := idx - 1
		if i >= len(found) {
			i = len(found) - 1
		}
		// For letters_03 with batches starting at 17_22, first batch is correct for part 3.
		// Using min(idx-1, len-1) works when batches are only the remaining ones.
		return found[min(i, len(found)-1)]
	}
	return found[0]
}

func partIndex(base string) int {
	// letters_03.md or chapter_02.md
	base = strings.TrimSuffix(base, ".md")
	i := strings.LastIndex(base, "_")
	if i < 0 || i+1 >= len(base) {
		return 0
	}
	n, err := strconv.Atoi(base[i+1:])
	if err != nil {
		return 0
	}
	return n
}

// buildNextBatchRequest builds a concrete continue prompt for the next missing part.
// thrash=true injects hard anti-search language for re-wake after search_text spam.
// ok=false when this goal has no progressive part gates (not a book job).
func buildNextBatchRequest(g goal.Goal, thrash bool) (request, problem string, ok bool) {
	part, found := nextMissingPart(g)
	if !found {
		// Fall back: final output gate only
		if final := nextMissingFinal(g); final.Path != "" {
			req := fmt.Sprintf(
				"【本輪任務 — 組裝最終輸出，禁止 decision=block】\n"+
					"將已完成 parts 合併為最終檔：\n%s\n"+
					"→ file_edit atomic_write 該路徑（#標題、無「待續/目前已完成前言/後續章節將」）\n"+
					"→ get → acquire_lease(worker_id=chatgpt-model) → commit_turn(decision=continue|verify)\n"+
					"禁止 search_text 空轉；禁止 decision=block。",
				final.Path,
			)
			if thrash {
				req = "【停止 search_text】只 read 已知 parts 路徑並組裝。\n" + req
			}
			return req, "assemble final output: " + final.Path, true
		}
		return "", "", false
	}
	src := findSrcBatchNearPart(part.Path)
	var b strings.Builder
	if thrash {
		b.WriteString("【停止 thrash】禁止 search_text / 全庫搜尋。只 read_file 下列已知路徑，然後 atomic_write，再 commit_turn。\n")
	} else {
		b.WriteString("【本輪任務 — 原文已就位，禁止 decision=block】\n")
	}
	b.WriteString(fmt.Sprintf("下一缺產物：%s（file_min_bytes>=%d）\n", part.Path, part.MinBytes))
	if src != "" {
		b.WriteString("英文來源（read_file 其一即可）:\n")
		b.WriteString(src)
		b.WriteString("\n")
	} else {
		b.WriteString("從 Goal Capsule / 既有 parts 延續翻譯；勿 decision=block 稱無原文。\n")
	}
	b.WriteString("必須:\n")
	if src != "" {
		b.WriteString("1) read_file 上述來源\n")
		b.WriteString("2) file_edit atomic_write → " + part.Path + "（#標題、無待續）\n")
		b.WriteString("3) goal_manage get → acquire_lease(worker_id=chatgpt-model) → commit_turn(decision=continue, reasoning_lease_id, expected_capsule_version)\n")
	} else {
		b.WriteString("1) file_edit atomic_write → " + part.Path + "\n")
		b.WriteString("2) get → acquire_lease(chatgpt-model) → commit_turn continue\n")
	}
	b.WriteString("禁止: decision=block、search_text 空轉、只讀不寫。\n")
	prob := fmt.Sprintf("%s missing or below %d bytes", filepath.Base(part.Path), part.MinBytes)
	if src != "" {
		prob += "; source ready at " + src
	}
	return b.String(), prob, true
}

func nextMissingFinal(g goal.Goal) partGate {
	for _, c := range g.SuccessCriteria {
		if c.ID != "final_bytes" && !strings.HasPrefix(c.ID, "final_") {
			continue
		}
		expr := strings.TrimSpace(c.Expression)
		if !strings.HasPrefix(expr, "file_min_bytes:") {
			continue
		}
		rest := strings.TrimPrefix(expr, "file_min_bytes:")
		i := strings.LastIndex(rest, ":")
		if i <= 0 {
			continue
		}
		path := rest[:i]
		n, err := strconv.ParseInt(rest[i+1:], 10, 64)
		if err != nil {
			n = 1
		}
		st, err := os.Stat(path)
		if err != nil || st.IsDir() || st.Size() < n {
			return partGate{ID: c.ID, Path: path, MinBytes: n}
		}
	}
	return partGate{}
}

// hasDurableBookProgress is true when parts/evidence already exist on disk.
func hasDurableBookProgress(g goal.Goal) bool {
	if trackedOutputBytes(g) > 0 {
		return true
	}
	if len(g.Evidence) > 0 {
		return true
	}
	for _, p := range bookPartGates(g) {
		if st, err := os.Stat(p.Path); err == nil && !st.IsDir() && st.Size() > 0 {
			return true
		}
	}
	return false
}

// isSoftRecoverableBlock reports model/progress blocks that orch may auto-resume
// when durable work already exists (false safety/no-source, no_progress).
// Hard operator blocks (permission, max ticks, wake failures) stay terminal.
func isSoftRecoverableBlock(blocker string) bool {
	b := strings.ToLower(strings.TrimSpace(blocker))
	if b == "" {
		return false
	}
	hard := []string{
		"tool permission",
		"permission auto-approve",
		"permission dialog",
		"max ticks",
		"wake/commit failed",
		"no mcp/tool activity after resume paste",
		"no commit_turn and no output growth",
		"login",
		"quota",
		"usage limit",
	}
	for _, h := range hard {
		if strings.Contains(b, h) {
			return false
		}
	}
	// Model decision=block summaries and progress detector.
	soft := []string{
		"no_progress",
		"decision=block",
		"block:",
		"安全",
		"safety",
		"無原文",
		"no source",
		"请使用者",
		"請使用者",
		"need_user",
		"原文",
		"letter",
		"書信",
	}
	for _, s := range soft {
		if strings.Contains(b, s) {
			return true
		}
	}
	// Default: if durable progress exists, caller still checks progress; treat unknown model blocks as soft.
	return true
}

const maxSoftUnblocks = 3
