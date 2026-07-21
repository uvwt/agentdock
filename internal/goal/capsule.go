package goal

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Capsule is the fixed-budget view handed to a reasoning worker.
type Capsule struct {
	GoalID           string             `json:"goal_id"`
	CapsuleVersion   int                `json:"capsule_version"`
	Title            string             `json:"title"`
	Objective        string             `json:"objective"`
	Status           Status             `json:"status"`
	Mode             Mode               `json:"mode"`
	WorkspaceID      string             `json:"workspace_id,omitempty"`
	DeviceID         string             `json:"device_id,omitempty"`
	BaseGitSHA       string             `json:"base_git_sha,omitempty"`
	CurrentGitSHA    string             `json:"current_git_sha,omitempty"`
	SuccessCriteria  []SuccessCriterion `json:"success_criteria"`
	Constraints      []Constraint       `json:"constraints,omitempty"`
	Budget           Budget             `json:"budget"`
	Completed        []string           `json:"completed,omitempty"`
	CurrentProblem   string             `json:"current_problem,omitempty"`
	CurrentRequest   string             `json:"current_request,omitempty"`
	Milestones       []Milestone        `json:"milestones,omitempty"`
	PendingSteps     []Step             `json:"pending_steps,omitempty"`
	Evidence         []EvidenceRef      `json:"evidence,omitempty"`
	PendingApprovals []Approval         `json:"pending_approvals,omitempty"`
	ActiveLease      *Lease             `json:"active_lease,omitempty"`
	Blocker          string             `json:"blocker,omitempty"`
	Summary          string             `json:"summary,omitempty"`
	ResumePrompt     string             `json:"resume_prompt"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

// BuildCapsule compiles a worker-facing capsule from durable goal state.
func BuildCapsule(g Goal) Capsule {
	pending := make([]Step, 0, len(g.Steps))
	for _, step := range g.Steps {
		if step.Status == StepPending || step.Status == StepInProgress || step.Status == StepFailed {
			pending = append(pending, step)
		}
	}
	// Keep capsule bounded: only recent evidence and open approvals.
	evidence := g.Evidence
	if len(evidence) > 12 {
		evidence = evidence[len(evidence)-12:]
	}
	approvals := make([]Approval, 0, len(g.PendingApprovals))
	for _, a := range g.PendingApprovals {
		if a.Status == "" || a.Status == "pending" {
			approvals = append(approvals, a)
		}
	}
	cap := Capsule{
		GoalID:           g.ID,
		CapsuleVersion:   g.CapsuleVersion,
		Title:            g.Title,
		Objective:        g.Objective,
		Status:           g.Status,
		Mode:             g.Mode,
		WorkspaceID:      g.WorkspaceID,
		DeviceID:         g.DeviceID,
		BaseGitSHA:       g.BaseGitSHA,
		CurrentGitSHA:    g.CurrentGitSHA,
		SuccessCriteria:  append([]SuccessCriterion(nil), g.SuccessCriteria...),
		Constraints:      append([]Constraint(nil), g.Constraints...),
		Budget:           g.Budget,
		Completed:        append([]string(nil), g.CompletedNotes...),
		CurrentProblem:   trimRunes(g.CurrentProblem, 800),
		CurrentRequest:   trimRunes(g.CurrentRequest, 800),
		Milestones:       append([]Milestone(nil), g.Milestones...),
		PendingSteps:     pending,
		Evidence:         append([]EvidenceRef(nil), evidence...),
		PendingApprovals: approvals,
		ActiveLease:      cloneLease(g.ActiveLease),
		Blocker:          g.Blocker,
		Summary:          trimRunes(g.Summary, 1200),
		UpdatedAt:        g.UpdatedAt,
	}
	cap.ResumePrompt = RenderResumePrompt(cap)
	return cap
}

// RenderResumePrompt builds the text a worker should paste into a conversation.
func RenderResumePrompt(c Capsule) string {
	var b strings.Builder
	fmt.Fprintf(&b, "/goal resume %s\n", c.GoalID)
	b.WriteString("你是此 Goal 的推理 Worker。\n")
	fmt.Fprintf(&b, "請先呼叫 goal_manage get，讀取 capsule_version=%d 的 Goal Capsule。\n", c.CapsuleVersion)
	b.WriteString("不要依賴目前對話以外的聊天歷史。\n")
	b.WriteString("工作策略：優先完成下一個可驗證小切片（例如單一章節 MD），不要一次提交整本未分段大任務。\n")
	b.WriteString("run_command 請用可執行命令或腳本路徑（如 python3 /tmp/.../build.py）。若腳本不存在，必須先 file_edit action=add 建立腳本，再 run_command。\n")
	b.WriteString("寫入長文請用 file_edit action=atomic_write（或先寫 .tmp 再 mv）；禁止先清空目標檔再慢慢寫。\n")
	if c.CurrentRequest != "" {
		b.WriteString("本輪任務：\n")
		b.WriteString(c.CurrentRequest)
		b.WriteByte('\n')
	} else if c.CurrentProblem != "" {
		b.WriteString("當前問題：\n")
		b.WriteString(c.CurrentProblem)
		b.WriteByte('\n')
	} else {
		b.WriteString("本輪任務：閱讀 Capsule，提交下一階段計畫。\n")
	}
	if tips := resumeEvidenceHints(c); tips != "" {
		b.WriteString("相關證據 / 產物：\n")
		b.WriteString(tips)
		b.WriteByte('\n')
	}
	if hasPendingManualCriteria(c) {
		b.WriteString("注意：仍有 manual 成功條件；需 evidence（criterion_id + satisfied=true），或 decision=block 請求使用者確認。\n")
	}
	b.WriteString("完成一個可驗證切片後必須：\n")
	b.WriteString("1) goal_manage get（讀最新 capsule_version）\n")
	b.WriteString("2) 若無 active_lease：goal_manage acquire_lease（worker_id 自取，例如 chatgpt-model）\n")
	b.WriteString("3) goal_manage commit_turn（帶 reasoning_lease_id + expected_capsule_version + decision/summary）\n")
	b.WriteString("不要只在聊天中提供建議；不要省略 commit_turn。\n")
	return b.String()
}

func hasPendingManualCriteria(c Capsule) bool {
	for _, sc := range c.SuccessCriteria {
		if sc.Type == CriterionManual && sc.Status != CriterionSatisfied {
			return true
		}
	}
	return false
}

func resumeEvidenceHints(c Capsule) string {
	if len(c.Evidence) == 0 {
		return ""
	}
	var lines []string
	for i := len(c.Evidence) - 1; i >= 0 && len(lines) < 4; i-- {
		ev := c.Evidence[i]
		line := "- " + firstNonEmptyStr(ev.Kind, "evidence") + ": " + trimRunes(ev.Summary, 180)
		if ev.URI != "" {
			line += " uri=" + ev.URI
		}
		if ev.Data != nil {
			if s, _ := ev.Data["stderr_tail"].(string); strings.TrimSpace(s) != "" {
				line += " stderr=" + trimRunes(s, 160)
			}
			if s, _ := ev.Data["command"].(string); strings.TrimSpace(s) != "" && ev.Kind == "command" {
				line += " cmd=" + trimRunes(s, 100)
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func trimRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

func cloneLease(l *Lease) *Lease {
	if l == nil {
		return nil
	}
	cp := *l
	return &cp
}
