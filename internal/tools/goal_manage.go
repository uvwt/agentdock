package tools

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/goal"
)

var goalActions = []string{
	"create", "list", "get", "commit_turn", "request_approval", "resolve_approval", "update_constraints",
	"pause", "resume", "cancel", "mark_blocked", "mark_completed", "get_evidence",
	"acquire_lease", "release_lease", "add_evidence", "verify", "check_policy",
	"execute_steps", "run_workflow", "bind", "unbind", "store_artifact",
	"request_reasoning", "chatgpt_wake", "chatgpt_worker_status", "set_auto_wake", "set_auto_approve_tools", "chatgpt_force_rotate", "chatgpt_force_rotate",
	"orchestrate_start", "orchestrate_stop", "orchestrate_status",
}

type goalManageInput struct {
	Action                 string
	GoalID                 string
	Title                  string
	Objective              string
	WorkspaceID            string
	DeviceID               string
	Mode                   string
	Status                 string
	Limit                  int
	Full                   bool
	WorkerID               string
	LeaseID                string
	LeaseTTLSeconds        int
	ExpectedCapsuleVersion int
	Decision               string
	Summary                string
	NextMilestone          string
	CurrentProblem         string
	CurrentRequest         string
	Reason                 string
	Tried                  string
	EvidenceText           string
	NeedUser               string
	ApprovalAction         string
	ApprovalRisk           string
	EvidenceIDs            []string
	CompletedNotes         []string
	SuccessCriteria        []goal.SuccessCriterionInput
	Constraints            []goal.Constraint
	Milestones             []goal.MilestoneInput
	Steps                  []goal.CommitStepInput
	EvidenceKind           string
	EvidenceSummary        string
	EvidenceURI            string
	EvidenceData           map[string]any
	Budget                 *goal.Budget
	BaseGitSHA             string
	ApprovalID             string
	ApprovalDecision       string
	PolicyAction           string
	PolicyTargets          []string
	Workflow               *goal.Workflow
	WorkDir                string
	ArtifactPath           string
	ArtifactFilename       string
	ArtifactText           string
	ArtifactContentType    string
	CriterionID            string
}

func parseGoalManageInput(args map[string]any) (goalManageInput, error) {
	input := goalManageInput{
		Action:                 strings.ToLower(strings.TrimSpace(stringArg(args, "action", ""))),
		GoalID:                 strings.TrimSpace(stringArg(args, "goal_id", "")),
		Title:                  stringArg(args, "title", ""),
		Objective:              stringArg(args, "objective", ""),
		WorkspaceID:            stringArg(args, "workspace_id", ""),
		DeviceID:               stringArg(args, "device_id", ""),
		Mode:                   strings.ToLower(strings.TrimSpace(stringArg(args, "mode", ""))),
		Status:                 strings.ToLower(strings.TrimSpace(stringArg(args, "status", ""))),
		Limit:                  intArg(args, "limit", 50),
		Full:                   boolArg(args, "full", false),
		WorkerID:               stringArg(args, "worker_id", ""),
		LeaseID:                strings.TrimSpace(stringArg(args, "reasoning_lease_id", "")),
		LeaseTTLSeconds:        intArg(args, "lease_ttl_seconds", 0),
		ExpectedCapsuleVersion: intArg(args, "expected_capsule_version", 0),
		Decision:               strings.ToLower(strings.TrimSpace(stringArg(args, "decision", ""))),
		Summary:                stringArg(args, "summary", ""),
		NextMilestone:          stringArg(args, "next_milestone", ""),
		CurrentProblem:         stringArg(args, "current_problem", ""),
		CurrentRequest:         stringArg(args, "current_request", ""),
		Reason:                 stringArg(args, "reason", ""),
		Tried:                  stringArg(args, "tried", ""),
		EvidenceText:           stringArg(args, "evidence_text", ""),
		NeedUser:               stringArg(args, "need_user", ""),
		ApprovalAction:         stringArg(args, "approval_action", ""),
		ApprovalRisk:           stringArg(args, "risk", ""),
		EvidenceIDs:            stringSliceArg(args, "evidence_ids"),
		CompletedNotes:         stringSliceArg(args, "completed"),
		EvidenceKind:           stringArg(args, "evidence_kind", ""),
		EvidenceSummary:        stringArg(args, "evidence_summary", ""),
		EvidenceURI:            stringArg(args, "evidence_uri", ""),
		BaseGitSHA:             stringArg(args, "base_git_sha", ""),
		ApprovalID:             strings.TrimSpace(stringArg(args, "approval_id", "")),
		ApprovalDecision:       strings.ToLower(strings.TrimSpace(stringArg(args, "approval_decision", ""))),
		PolicyAction:           strings.TrimSpace(stringArg(args, "policy_action", "")),
		PolicyTargets:          stringSliceArg(args, "policy_targets"),
		WorkDir:                stringArg(args, "work_dir", ""),
		ArtifactPath:           stringArg(args, "artifact_path", ""),
		ArtifactFilename:       stringArg(args, "artifact_filename", ""),
		ArtifactText:           stringArg(args, "artifact_text", ""),
		ArtifactContentType:    stringArg(args, "artifact_content_type", ""),
		CriterionID:            stringArg(args, "criterion_id", ""),
	}
	// Alias: lease_id accepted as well as reasoning_lease_id.
	if input.LeaseID == "" {
		input.LeaseID = strings.TrimSpace(stringArg(args, "lease_id", ""))
	}
	if raw := args["success_criteria"]; raw != nil {
		if err := remarshal(raw, &input.SuccessCriteria); err != nil {
			return input, toolErrorDetails("VALIDATION_ERROR", "success_criteria must be an array of objects", "validation", map[string]any{"field": "success_criteria", "reason": err.Error()})
		}
	}
	if raw := args["constraints"]; raw != nil {
		if err := remarshal(raw, &input.Constraints); err != nil {
			return input, toolErrorDetails("VALIDATION_ERROR", "constraints must be an array of objects", "validation", map[string]any{"field": "constraints", "reason": err.Error()})
		}
	}
	if raw := args["milestones"]; raw != nil {
		if err := remarshal(raw, &input.Milestones); err != nil {
			return input, toolErrorDetails("VALIDATION_ERROR", "milestones must be an array of objects", "validation", map[string]any{"field": "milestones", "reason": err.Error()})
		}
	}
	if raw := args["steps"]; raw != nil {
		if err := remarshal(raw, &input.Steps); err != nil {
			return input, toolErrorDetails("VALIDATION_ERROR", "steps must be an array of commit steps", "validation", map[string]any{"field": "steps", "reason": err.Error()})
		}
	}
	if raw := args["budget"]; raw != nil {
		var b goal.Budget
		if err := remarshal(raw, &b); err != nil {
			return input, toolErrorDetails("VALIDATION_ERROR", "budget must be an object", "validation", map[string]any{"field": "budget", "reason": err.Error()})
		}
		input.Budget = &b
	}
	if raw := args["evidence_data"]; raw != nil {
		if err := remarshal(raw, &input.EvidenceData); err != nil {
			return input, toolErrorDetails("VALIDATION_ERROR", "evidence_data must be an object", "validation", map[string]any{"field": "evidence_data", "reason": err.Error()})
		}
	}
	if raw := args["workflow"]; raw != nil {
		var wf goal.Workflow
		if err := remarshal(raw, &wf); err != nil {
			return input, toolErrorDetails("VALIDATION_ERROR", "workflow must be an object with steps", "validation", map[string]any{"field": "workflow", "reason": err.Error()})
		}
		input.Workflow = &wf
	}
	return input, nil
}

func (r *Runtime) goalManage(ctx context.Context, args map[string]any) (Result, error) {
	if r.goals == nil {
		return nil, toolError("GOAL_UNAVAILABLE", "goal store is not initialized", "runtime")
	}
	input, err := parseGoalManageInput(args)
	if err != nil {
		return nil, err
	}
	switch input.Action {
	case "create":
		mode := goal.Mode(input.Mode)
		createIn := goal.CreateInput{
			Title:           input.Title,
			Objective:       input.Objective,
			WorkspaceID:     input.WorkspaceID,
			DeviceID:        input.DeviceID,
			Mode:            mode,
			SuccessCriteria: input.SuccessCriteria,
			Constraints:     input.Constraints,
			Budget:          input.Budget,
			Milestones:      input.Milestones,
			BaseGitSHA:      input.BaseGitSHA,
		}
		// Auto-apply progressive chapter/letter milestones for book-scale translation goals
		// when the caller did not already supply a rich milestone plan.
		if len(createIn.Milestones) == 0 {
			if tmpl, ok := goal.SuggestBookJobFromObjective(createIn.Title, createIn.Objective, ""); ok {
				// Best-effort output path extraction from objective (…輸出…md / path ending .md)
				outHint := extractMarkdownPathHint(createIn.Objective)
				tmpl.OutputPath = outHint
				if strings.TrimSpace(tmpl.SourcePDF) == "" {
					tmpl.SourcePDF = extractPDFPathHint(createIn.Objective)
				}
				goal.ApplyBookJobTemplate(&createIn, tmpl)
			}
		}
		created, err := r.goals.Create(createIn)
		if err != nil {
			return nil, goalToolError(err)
		}
		r.SetActiveGoalID(created.ID)
		return Result{
			"action":               input.Action,
			"goal_id":              created.ID,
			"capsule":              goal.BuildCapsule(created),
			"state_dir":            r.goals.Root(),
			"active_goal_id":       created.ID,
			"next_required_action": "Review the capsule, acquire_lease with a worker_id, then commit_turn with a structured plan. High-risk tools are policy-gated while this goal is bound.",
		}, nil

	case "list":
		var status goal.Status
		if input.Status != "" {
			status = goal.Status(input.Status)
		}
		items, err := r.goals.List(status, input.Limit)
		if err != nil {
			return nil, goalToolError(err)
		}
		summaries := make([]map[string]any, 0, len(items))
		for _, g := range items {
			summaries = append(summaries, compactGoalSummary(g))
		}
		return Result{"action": input.Action, "goals": summaries, "count": len(summaries), "state_dir": r.goals.Root()}, nil

	case "get":
		g, err := r.goals.Get(input.GoalID)
		if err != nil {
			return nil, goalToolError(err)
		}
		out := Result{
			"action":  input.Action,
			"goal_id": g.ID,
			"capsule": goal.BuildCapsule(g),
		}
		if input.Full {
			out["goal"] = g
		}
		return out, nil

	case "acquire_lease":
		ttl := time.Duration(input.LeaseTTLSeconds) * time.Second
		g, lease, err := r.goals.AcquireLease(input.GoalID, input.WorkerID, ttl)
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{
			"action":  input.Action,
			"goal_id": g.ID,
			"lease":   lease,
			"capsule": goal.BuildCapsule(g),
		}, nil

	case "release_lease":
		g, err := r.goals.ReleaseLease(input.GoalID, input.LeaseID)
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{"action": input.Action, "goal_id": g.ID, "goal_summary": compactGoalSummary(g)}, nil

	case "commit_turn":
		g, err := r.goals.CommitTurn(goal.CommitTurnInput{
			GoalID:                 input.GoalID,
			ReasoningLeaseID:       input.LeaseID,
			ExpectedCapsuleVersion: input.ExpectedCapsuleVersion,
			Decision:               goal.Decision(input.Decision),
			Summary:                input.Summary,
			NextMilestone:          input.NextMilestone,
			CurrentProblem:         input.CurrentProblem,
			CurrentRequest:         input.CurrentRequest,
			Steps:                  input.Steps,
			CompletedNotes:         input.CompletedNotes,
		})
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{
			"action":       input.Action,
			"goal_id":      g.ID,
			"goal_summary": compactGoalSummary(g),
			"capsule":      goal.BuildCapsule(g),
		}, nil

	case "request_approval":
		g, err := r.goals.RequestApproval(goal.RequestApprovalInput{
			GoalID:  input.GoalID,
			Action:  firstNonEmpty(input.ApprovalAction, input.Summary),
			Summary: firstNonEmpty(input.Summary, input.ApprovalAction),
			Risk:    input.ApprovalRisk,
		})
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{"action": input.Action, "goal_id": g.ID, "goal_summary": compactGoalSummary(g), "capsule": goal.BuildCapsule(g)}, nil

	case "update_constraints":
		g, err := r.goals.UpdateConstraints(input.GoalID, input.Constraints)
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{"action": input.Action, "goal_id": g.ID, "goal_summary": compactGoalSummary(g), "capsule": goal.BuildCapsule(g)}, nil

	case "pause":
		g, err := r.goals.Pause(input.GoalID, input.Summary)
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{"action": input.Action, "goal_id": g.ID, "goal_summary": compactGoalSummary(g)}, nil

	case "resume":
		g, err := r.goals.Resume(input.GoalID, input.Summary)
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{"action": input.Action, "goal_id": g.ID, "capsule": goal.BuildCapsule(g)}, nil

	case "cancel":
		g, err := r.goals.Cancel(input.GoalID, input.Summary)
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{"action": input.Action, "goal_id": g.ID, "goal_summary": compactGoalSummary(g)}, nil

	case "mark_blocked":
		g, err := r.goals.MarkBlocked(goal.MarkBlockedInput{
			GoalID:   input.GoalID,
			Reason:   firstNonEmpty(input.Reason, input.Summary),
			Tried:    input.Tried,
			Evidence: input.EvidenceText,
			NeedUser: input.NeedUser,
		})
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{"action": input.Action, "goal_id": g.ID, "goal_summary": compactGoalSummary(g), "capsule": goal.BuildCapsule(g)}, nil

	case "mark_completed":
		g, err := r.goals.MarkCompleted(input.GoalID, input.Summary, input.EvidenceIDs)
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{"action": input.Action, "goal_id": g.ID, "goal_summary": compactGoalSummary(g)}, nil

	case "get_evidence":
		g, err := r.goals.Get(input.GoalID)
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{
			"action":   input.Action,
			"goal_id":  g.ID,
			"evidence": g.Evidence,
			"count":    len(g.Evidence),
		}, nil

	case "add_evidence":
		g, err := r.goals.AddEvidence(input.GoalID, goal.EvidenceRef{
			Kind:    input.EvidenceKind,
			Summary: firstNonEmpty(input.EvidenceSummary, input.Summary),
			URI:     input.EvidenceURI,
			Data:    input.EvidenceData,
		})
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{"action": input.Action, "goal_id": g.ID, "evidence": g.Evidence, "goal_summary": compactGoalSummary(g)}, nil

	case "resolve_approval":
		g, err := r.goals.ResolveApproval(input.GoalID, input.ApprovalID, firstNonEmpty(input.ApprovalDecision, input.Decision), input.Summary)
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{"action": input.Action, "goal_id": g.ID, "goal_summary": compactGoalSummary(g), "capsule": goal.BuildCapsule(g)}, nil

	case "verify":
		g, report, err := r.goals.VerifyGoal(input.GoalID)
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{"action": input.Action, "goal_id": g.ID, "verify": report, "goal_summary": compactGoalSummary(g), "capsule": goal.BuildCapsule(g)}, nil

	case "check_policy":
		g, err := r.goals.Get(input.GoalID)
		if err != nil {
			return nil, goalToolError(err)
		}
		action := goal.StepAction(firstNonEmpty(input.PolicyAction, "run_command"))
		decision := goal.CheckPolicy(g, action, input.PolicyTargets)
		return Result{"action": input.Action, "goal_id": g.ID, "policy": decision}, nil

	case "execute_steps":
		g, err := r.goals.Get(input.GoalID)
		if err != nil {
			return nil, goalToolError(err)
		}
		runner := &goal.Runner{WorkDir: firstNonEmpty(input.WorkDir, r.ws.Root())}
		result := runner.ExecuteGoalSteps(ctx, g)
		g, err = r.goals.ApplyExecution(input.GoalID, result)
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{
			"action": input.Action, "goal_id": g.ID, "run": result,
			"goal_summary": compactGoalSummary(g), "capsule": goal.BuildCapsule(g),
		}, nil

	case "run_workflow":
		if input.Workflow == nil || len(input.Workflow.Steps) == 0 {
			return nil, toolErrorDetails("VALIDATION_ERROR", "workflow.steps is required", "validation", map[string]any{"field": "workflow"})
		}
		g, err := r.goals.Get(input.GoalID)
		if err != nil {
			return nil, goalToolError(err)
		}
		runner := &goal.Runner{WorkDir: firstNonEmpty(input.WorkDir, r.ws.Root())}
		result := runner.RunWorkflow(ctx, g, *input.Workflow)
		g, err = r.goals.ApplyExecution(input.GoalID, result)
		if err != nil {
			return nil, goalToolError(err)
		}
		return Result{
			"action": input.Action, "goal_id": g.ID, "run": result,
			"goal_summary": compactGoalSummary(g), "capsule": goal.BuildCapsule(g),
		}, nil

	case "bind":
		if input.GoalID == "" {
			return nil, toolErrorDetails("VALIDATION_ERROR", "goal_id is required for bind", "validation", map[string]any{"field": "goal_id"})
		}
		if _, err := r.goals.Get(input.GoalID); err != nil {
			return nil, goalToolError(err)
		}
		r.SetActiveGoalID(input.GoalID)
		return Result{"action": input.Action, "goal_id": input.GoalID, "active_goal_id": input.GoalID}, nil

	case "unbind":
		prev := r.ActiveGoalID()
		r.SetActiveGoalID("")
		return Result{"action": input.Action, "previous_goal_id": prev, "active_goal_id": ""}, nil

	case "store_artifact":
		if r.artifacts == nil {
			return nil, toolError("ARTIFACT_UNAVAILABLE", "artifact store is not initialized", "runtime")
		}
		var meta goal.ArtifactMeta
		var err error
		if path := strings.TrimSpace(input.ArtifactPath); path != "" {
			meta, err = r.artifacts.PutFile(path, firstNonEmpty(input.EvidenceKind, "file"), firstNonEmpty(input.EvidenceSummary, input.Summary, path))
		} else if textBody := input.ArtifactText; textBody != "" {
			name := firstNonEmpty(input.ArtifactFilename, "artifact.txt")
			ct := firstNonEmpty(input.ArtifactContentType, "text/plain; charset=utf-8")
			meta, err = r.artifacts.PutBytes(name, []byte(textBody), firstNonEmpty(input.EvidenceKind, "text"), firstNonEmpty(input.EvidenceSummary, input.Summary, name), ct)
		} else {
			return nil, toolErrorDetails("VALIDATION_ERROR", "artifact_path or artifact_text is required", "validation", map[string]any{})
		}
		if err != nil {
			return nil, goalToolError(err)
		}
		out := Result{"action": input.Action, "artifact": meta}
		if input.GoalID != "" {
			ev := goal.EvidenceFromMeta(meta, input.CriterionID, input.EvidenceData)
			g, err := r.goals.AddEvidence(input.GoalID, ev)
			if err != nil {
				return nil, goalToolError(err)
			}
			out["goal_id"] = g.ID
			out["evidence"] = g.Evidence
			out["goal_summary"] = compactGoalSummary(g)
		}
		return out, nil

	case "request_reasoning":
		return r.RuntimeRequestReasoning(input.GoalID, firstNonEmpty(input.CurrentRequest, input.Summary), firstNonEmpty(input.CurrentProblem, input.Reason))

	case "chatgpt_wake":
		return r.RuntimeChatGPTWake(ctx, input.GoalID)

	case "chatgpt_worker_status":
		return r.RuntimeChatGPTWorkerStatus(), nil

	case "set_auto_wake":
		enabled := boolArg(args, "auto_wake", true)
		if _, ok := args["enabled"]; ok {
			enabled = boolArg(args, "enabled", true)
		}
		return r.RuntimeSetChatGPTAutoWake(enabled), nil

	case "set_auto_approve_tools":
		enabled := boolArg(args, "auto_approve_tools", false)
		if _, ok := args["enabled"]; ok {
			enabled = boolArg(args, "enabled", false)
		}
		return r.RuntimeSetChatGPTAutoApproveTools(enabled), nil

	case "chatgpt_force_rotate":
		return r.RuntimeChatGPTForceRotate(), nil

	case "orchestrate_start":
		return r.RuntimeOrchestratorStart(input.GoalID)

	case "orchestrate_stop":
		return r.RuntimeOrchestratorStop(input.GoalID), nil

	case "orchestrate_status":
		return r.RuntimeOrchestratorStatus(input.GoalID), nil

	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported goal_manage action", "validation", map[string]any{"action": input.Action, "allowed": goalActions})
	}
}

func compactGoalSummary(g goal.Goal) map[string]any {
	pendingCriteria := 0
	for _, c := range g.SuccessCriteria {
		if c.Status == goal.CriterionPending || c.Status == goal.CriterionFailed {
			pendingCriteria++
		}
	}
	pendingSteps := 0
	for _, s := range g.Steps {
		if s.Status == goal.StepPending || s.Status == goal.StepInProgress {
			pendingSteps++
		}
	}
	out := map[string]any{
		"goal_id":          g.ID,
		"title":            g.Title,
		"status":           g.Status,
		"mode":             g.Mode,
		"capsule_version":  g.CapsuleVersion,
		"workspace_id":     g.WorkspaceID,
		"pending_criteria": pendingCriteria,
		"pending_steps":    pendingSteps,
		"reasoning_turns":  g.Budget.ReasoningTurnsUsed,
		"updated_at":       g.UpdatedAt,
	}
	if g.Summary != "" {
		out["summary"] = g.Summary
	}
	if g.Blocker != "" {
		out["blocker"] = g.Blocker
	}
	if g.CurrentProblem != "" {
		out["current_problem"] = g.CurrentProblem
	}
	if g.CurrentRequest != "" {
		out["current_request"] = g.CurrentRequest
	}
	if g.WorkerConversationURL != "" {
		out["worker_conversation_url"] = g.WorkerConversationURL
	}
	if g.WorkerConversationID != "" {
		out["worker_conversation_id"] = g.WorkerConversationID
	}
	if g.ActiveLease != nil {
		out["active_lease_id"] = g.ActiveLease.LeaseID
		out["active_worker_id"] = g.ActiveLease.WorkerID
	}
	return out
}

func goalToolError(err error) error {
	if err == nil {
		return nil
	}
	var conflict *goal.ConflictError
	if errors.As(err, &conflict) {
		return toolErrorDetails(conflict.Code, conflict.Error(), "conflict", map[string]any{
			"goal_id":          conflict.GoalID,
			"expected_version": conflict.ExpectedVersion,
			"current_version":  conflict.CurrentVersion,
			"lease_id":         conflict.LeaseID,
			"active_lease_id":  conflict.ActiveLeaseID,
			"retryable":        true,
			"next_action":      "goal_manage get to read the latest capsule, then acquire_lease and commit_turn with the new capsule_version",
		})
	}
	switch {
	case errors.Is(err, goal.ErrGoalNotFound):
		return toolErrorDetails("GOAL_NOT_FOUND", err.Error(), "not_found", map[string]any{})
	case errors.Is(err, goal.ErrLeaseRequired):
		return toolErrorDetails("LEASE_REQUIRED", err.Error(), "validation", map[string]any{
			"next_action": "call goal_manage acquire_lease before commit_turn",
		})
	case errors.Is(err, goal.ErrLeaseExpired):
		return toolErrorDetails("LEASE_EXPIRED", err.Error(), "conflict", map[string]any{
			"retryable":   true,
			"next_action": "acquire_lease again and commit with the current capsule_version",
		})
	case errors.Is(err, goal.ErrBudgetExceeded):
		return toolErrorDetails("BUDGET_EXCEEDED", err.Error(), "policy", map[string]any{
			"next_action": "stop automatic looping; mark_blocked or ask the user to raise budget",
		})
	case errors.Is(err, goal.ErrVerifyFailed):
		return toolErrorDetails("VERIFY_FAILED", err.Error(), "verification", map[string]any{
			"next_action": "add structured evidence for unmet criteria, call verify, then mark_completed",
		})
	case errors.Is(err, goal.ErrPolicyDenied), errors.Is(err, goal.ErrApprovalRequired):
		return toolErrorDetails("POLICY_DENIED", err.Error(), "policy", map[string]any{
			"next_action": "request_approval / resolve_approval, or choose a permitted action",
		})
	case errors.Is(err, goal.ErrInvalidInput), errors.Is(err, goal.ErrInvalidStatus):
		return toolErrorDetails("VALIDATION_ERROR", err.Error(), "validation", map[string]any{})
	case errors.Is(err, goal.ErrStateConflict):
		return toolErrorDetails("STATE_CONFLICT", err.Error(), "conflict", map[string]any{"retryable": true})
	default:
		return toolErrorCause("GOAL_ERROR", err.Error(), "runtime", map[string]any{}, err)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func extractMarkdownPathHint(objective string) string {
	// Prefer explicit absolute/home paths ending in .md even when they contain spaces.
	// Example: /Users/.../RSSB/《靈性書信》Spiritual-Letters-Jaimal Singh_繁體中文.md
	reMD := regexp.MustCompile(`(?:~|/Users/|/[a-zA-Z0-9._\-]+)[^\n\r"'「」]{0,240}?\.md`)
	if m := reMD.FindString(objective); m != "" {
		return strings.Trim(m, " \t\"'`「」，。；:")
	}
	// Fallback: token scan (no spaces)
	for _, f := range strings.Fields(objective) {
		f = strings.Trim(f, "\"'` ,，。；:")
		if strings.HasSuffix(strings.ToLower(f), ".md") && (strings.Contains(f, "/") || strings.HasPrefix(f, "~")) {
			return f
		}
	}
	return ""
}

func extractPDFPathHint(objective string) string {
	rePDF := regexp.MustCompile(`(?:~|/Users/|/[a-zA-Z0-9._\-]+)[^\n\r"'「」]{0,240}?\.pdf`)
	if m := rePDF.FindString(objective); m != "" {
		return strings.Trim(m, " \t\"'`「」，。；:")
	}
	for _, f := range strings.Fields(objective) {
		f = strings.Trim(f, "\"'` ,，。；:")
		if strings.HasSuffix(strings.ToLower(f), ".pdf") {
			return f
		}
	}
	return ""
}

