package goal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BookJobKind selects progressive milestone templates for long translation jobs.
type BookJobKind string

const (
	BookJobChapter BookJobKind = "chapter"
	BookJobLetter  BookJobKind = "letter"
)

// BookJobTemplateInput configures progressive milestones + criteria for book-scale goals.
type BookJobTemplateInput struct {
	Kind          BookJobKind
	SourcePDF     string
	OutputPath    string // final assembled markdown path
	PartsDir      string // directory for intermediate part files
	PartCount     int    // number of chapters/letter-batches
	PartMinBytes  int    // min bytes per intermediate part (default 8000)
	FinalMinBytes int    // min bytes for final output (default 80000)
	FinalMinLines int    // min lines for final output (default 500)
}

// ApplyBookJobTemplate mutates CreateInput with milestones and progressive success criteria.
// It does not clear existing criteria; it appends progressive gates and milestones.
func ApplyBookJobTemplate(in *CreateInput, tmpl BookJobTemplateInput) {
	if in == nil {
		return
	}
	if tmpl.PartCount <= 0 {
		tmpl.PartCount = 4
	}
	if tmpl.PartMinBytes <= 0 {
		tmpl.PartMinBytes = 8000
	}
	if tmpl.FinalMinBytes <= 0 {
		tmpl.FinalMinBytes = 80000
	}
	if tmpl.FinalMinLines <= 0 {
		tmpl.FinalMinLines = 500
	}
	if strings.TrimSpace(tmpl.PartsDir) == "" {
		if strings.TrimSpace(tmpl.OutputPath) != "" {
			tmpl.PartsDir = filepath.Join(filepath.Dir(tmpl.OutputPath), "parts")
		} else {
			tmpl.PartsDir = filepath.Join(os.TempDir(), "agentdock-goal-parts")
		}
	}
	kindLabel := "章節"
	partPrefix := "chapter"
	if tmpl.Kind == BookJobLetter {
		kindLabel = "書信批次"
		partPrefix = "letters"
	}

	// Milestones: prep + N parts + assemble/verify
	ms := []MilestoneInput{{ID: "m_prep", Title: "預檢來源與抽取正文"}}
	for i := 1; i <= tmpl.PartCount; i++ {
		ms = append(ms, MilestoneInput{
			ID:    fmt.Sprintf("m_part_%02d", i),
			Title: fmt.Sprintf("翻譯%s %d/%d", kindLabel, i, tmpl.PartCount),
		})
	}
	ms = append(ms, MilestoneInput{ID: "m_assemble", Title: "合併各段並最終校驗"})
	if len(in.Milestones) == 0 {
		in.Milestones = ms
	} else {
		in.Milestones = append(in.Milestones, ms...)
	}

	// Progressive criteria for each part file + final output.
	var crit []SuccessCriterionInput
	for i := 1; i <= tmpl.PartCount; i++ {
		partPath := filepath.Join(tmpl.PartsDir, fmt.Sprintf("%s_%02d.md", partPrefix, i))
		crit = append(crit,
			SuccessCriterionInput{
				ID:         fmt.Sprintf("p%02d_bytes", i),
				Type:       CriterionCommand,
				Expression: fmt.Sprintf("file_min_bytes:%s:%d", partPath, tmpl.PartMinBytes),
			},
			SuccessCriterionInput{
				ID:         fmt.Sprintf("p%02d_heading", i),
				Type:       CriterionCommand,
				Expression: fmt.Sprintf("grep -q '^#' '%s'", partPath),
			},
			SuccessCriterionInput{
				ID:         fmt.Sprintf("p%02d_not_partial", i),
				Type:       CriterionCommand,
				Expression: fmt.Sprintf("file_not_contains:%s:待續", partPath),
			},
		)
	}
	if strings.TrimSpace(tmpl.OutputPath) != "" {
		crit = append(crit,
			SuccessCriterionInput{ID: "final_bytes", Type: CriterionCommand, Expression: fmt.Sprintf("file_min_bytes:%s:%d", tmpl.OutputPath, tmpl.FinalMinBytes)},
			SuccessCriterionInput{ID: "final_lines", Type: CriterionCommand, Expression: fmt.Sprintf("file_min_lines:%s:%d", tmpl.OutputPath, tmpl.FinalMinLines)},
			SuccessCriterionInput{ID: "final_heading", Type: CriterionCommand, Expression: fmt.Sprintf("grep -q '^#' '%s'", tmpl.OutputPath)},
			SuccessCriterionInput{ID: "final_not_preface_only", Type: CriterionCommand, Expression: fmt.Sprintf("file_not_contains:%s:目前已完成前言", tmpl.OutputPath)},
			SuccessCriterionInput{ID: "final_not_todo", Type: CriterionCommand, Expression: fmt.Sprintf("file_not_contains:%s:後續章節將", tmpl.OutputPath)},
			SuccessCriterionInput{ID: "final_not_partial", Type: CriterionCommand, Expression: fmt.Sprintf("file_not_contains:%s:待續", tmpl.OutputPath)},
		)
	}
	in.SuccessCriteria = append(in.SuccessCriteria, crit...)

	// Constraints for atomic write / no empty clobber.
	in.Constraints = append(in.Constraints,
		Constraint{Type: ConstraintQuality, Value: "長文請用 file_edit action=atomic_write 或 .tmp 後 mv；禁止先清空目標檔再寫"},
		Constraint{Type: ConstraintQuality, Value: fmt.Sprintf("分段產物目錄：%s；完成一段就驗證一段，再組裝到最終輸出", tmpl.PartsDir)},
		Constraint{Type: ConstraintQuality, Value: "若腳本 run_command 回 SCRIPT_MISSING，先 file_edit 建立腳本再執行"},
	)
	if strings.TrimSpace(tmpl.SourcePDF) != "" {
		in.Constraints = append(in.Constraints, Constraint{Type: ConstraintProhibition, Value: "不得修改原始 PDF: " + tmpl.SourcePDF})
	}
}

// SuggestBookJobFromObjective heuristically detects book translation jobs.
func SuggestBookJobFromObjective(title, objective, outputHint string) (BookJobTemplateInput, bool) {
	blob := strings.ToLower(title + " " + objective + " " + outputHint)
	out := BookJobTemplateInput{OutputPath: strings.TrimSpace(outputHint), PartCount: 4}
	if strings.Contains(blob, "letter") || strings.Contains(objective, "書信") {
		out.Kind = BookJobLetter
		out.PartCount = 5
		return out, true
	}
	if strings.Contains(blob, "chapter") || strings.Contains(objective, "章") || strings.Contains(objective, "gita") || strings.Contains(objective, "翻譯") {
		out.Kind = BookJobChapter
		out.PartCount = 6
		return out, true
	}
	return BookJobTemplateInput{}, false
}
