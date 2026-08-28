package acp

import (
	"context"
	"time"

	acpruntime "github.com/uvwt/agentdock/internal/acp"
)

func (s *Service) Session(ctx context.Context, args map[string]any) (Result, error) {
	if s == nil || s.manager == nil {
		return nil, validationError("ACP_NOT_CONFIGURED", "ACP runtime is not configured", nil)
	}
	action := actionArg(args)
	switch action {
	case "info":
		info, err := s.manager.AgentInfo(ctx)
		if err != nil {
			return nil, acpToolError(err)
		}
		policies := acpruntime.CurrentPolicies()
		return Result{
			"action": action, "agent": info.AgentInfo, "capabilities": info.AgentCapabilities,
			"protocol_version": info.ProtocolVersion, "auth_methods": append([]any{}, info.AuthMethods...),
			"context_policy": policies.Context, "event_policy": policies.Events,
			"interaction_policy": policies.Interactions, "steering_policy": policies.Steering,
		}, nil
	case "authenticate":
		methodID := stringArg(args, "auth_method_id", "")
		if err := s.manager.Authenticate(ctx, methodID); err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "auth_method_id": methodID, "authenticated": true}, nil
	case "new":
		result, err := s.manager.NewSession(ctx, stringArg(args, "cwd", ""), stringSliceArg(args, "additional_directories"))
		if err != nil {
			return nil, acpToolError(err)
		}
		return sessionActionResult(action, result), nil
	case "load":
		result, err := s.manager.LoadSession(ctx, stringArg(args, "session_id", ""))
		if err != nil {
			return nil, acpToolError(err)
		}
		return sessionActionResult(action, result), nil
	case "resume":
		result, err := s.manager.ResumeSession(ctx, stringArg(args, "session_id", ""))
		if err != nil {
			return nil, acpToolError(err)
		}
		return sessionActionResult(action, result), nil
	case "fork":
		var additional []string
		if _, present := args["additional_directories"]; present {
			additional = append([]string{}, stringSliceArg(args, "additional_directories")...)
		}
		result, err := s.manager.ForkSession(ctx, stringArg(args, "session_id", ""), stringArg(args, "cwd", ""), additional)
		if err != nil {
			return nil, acpToolError(err)
		}
		return sessionActionResult(action, result), nil
	case "set_mode":
		sessionID := stringArg(args, "session_id", "")
		modeID := stringArg(args, "mode_id", "")
		if err := s.manager.SetSessionMode(ctx, sessionID, modeID); err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "session_id": sessionID, "mode_id": modeID, "changed": true}, nil
	case "set_config":
		sessionID := stringArg(args, "session_id", "")
		configID := stringArg(args, "config_id", "")
		configOptions, err := s.manager.SetSessionConfigOption(ctx, sessionID, configID, args["config_value"])
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "session_id": sessionID, "config_id": configID, "config_options": configOptions, "changed": true}, nil
	case "list":
		sessions, err := s.manager.ListSessions()
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "sessions": sessions, "count": len(sessions)}, nil
	case "inspect":
		session, err := s.manager.InspectSession(stringArg(args, "session_id", ""))
		if err != nil {
			return nil, acpToolError(err)
		}
		messages, err := s.manager.SessionMessages(session.ID)
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "session": session, "messages": messages}, nil
	case "close":
		session, err := s.manager.CloseSession(ctx, stringArg(args, "session_id", ""))
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "session": session}, nil
	case "delete":
		sessionID := stringArg(args, "session_id", "")
		if err := s.manager.DeleteSession(ctx, sessionID); err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "session_id": sessionID, "deleted": true}, nil
	default:
		return nil, validationError("ACP_ACTION_INVALID", "unsupported ACP session action", map[string]any{"action": action})
	}
}

func sessionActionResult(action string, result acpruntime.SessionResult) Result {
	response := Result{"action": action, "session": result.Session, "agent": result.Agent}
	if result.Modes != nil {
		response["modes"] = result.Modes
	}
	if result.ConfigOptions != nil {
		response["config_options"] = result.ConfigOptions
	}
	return response
}

func (s *Service) Prompt(ctx context.Context, args map[string]any) (Result, error) {
	if s == nil || s.manager == nil {
		return nil, validationError("ACP_NOT_CONFIGURED", "ACP runtime is not configured", nil)
	}
	action := actionArg(args)
	switch action {
	case "start":
		result, err := s.manager.StartPrompt(ctx, stringArg(args, "session_id", ""), stringArg(args, "text", ""))
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{
			"action": action, "run_id": result.RunID, "session_id": result.SessionID,
			"status": result.Status, "started_at": result.StartedAt,
		}, nil
	case "events":
		after := intArg(args, "after_seq", 0)
		if after < 0 {
			return nil, validationError("ACP_AFTER_SEQ_INVALID", "after_seq must not be negative", map[string]any{"after_seq": after})
		}
		limit := intArg(args, "limit", 100)
		waitMS := intArg(args, "wait_ms", 0)
		if waitMS < 0 {
			return nil, validationError("ACP_WAIT_INVALID", "wait_ms must not be negative", map[string]any{"wait_ms": waitMS})
		}
		if waitMS > 25000 {
			waitMS = 25000
		}
		result, err := s.manager.PromptEvents(ctx, stringArg(args, "run_id", ""), uint64(after), limit, time.Duration(waitMS)*time.Millisecond)
		if err != nil {
			return nil, acpToolError(err)
		}
		response := Result{
			"action": action, "run_id": result.RunID, "session_id": result.SessionID,
			"status": result.Status, "events": result.Events, "next_seq": result.NextSeq,
			"first_seq": result.FirstSeq, "latest_seq": result.LatestSeq,
			"dropped_count": result.DroppedCount, "has_more": result.HasMore, "truncated": result.Truncated,
			"started_at":  result.StartedAt,
			"stop_reason": result.StopReason, "error_code": result.ErrorCode, "message": result.Message,
		}
		if result.EndedAt != nil {
			response["ended_at"] = result.EndedAt
		}
		return response, nil
	case "steer":
		result, err := s.manager.Steer(ctx, stringArg(args, "session_id", ""), stringArg(args, "text", ""))
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "session_id": stringArg(args, "session_id", ""), "steering": result}, nil
	case "cancel":
		sessionID := stringArg(args, "session_id", "")
		runID := stringArg(args, "run_id", "")
		if sessionID == "" && runID == "" {
			return nil, validationError("ACP_CANCEL_TARGET_REQUIRED", "session_id or run_id is required for cancel", nil)
		}
		if err := s.manager.CancelPrompt(ctx, sessionID, runID); err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "session_id": sessionID, "run_id": runID, "cancel_requested": true}, nil
	default:
		return nil, validationError("ACP_ACTION_INVALID", "unsupported ACP prompt action", map[string]any{"action": action})
	}
}

func (s *Service) Interaction(_ context.Context, args map[string]any) (Result, error) {
	if s == nil || s.manager == nil {
		return nil, validationError("ACP_NOT_CONFIGURED", "ACP runtime is not configured", nil)
	}
	action := actionArg(args)
	switch action {
	case "list":
		interactions := s.manager.ListInteractions(stringArg(args, "session_id", ""), boolArg(args, "pending_only", true))
		return Result{"action": action, "interactions": interactions, "count": len(interactions)}, nil
	case "inspect":
		interaction, err := s.manager.InspectInteraction(stringArg(args, "interaction_id", ""))
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "interaction": interaction}, nil
	case "respond":
		interaction, err := s.manager.RespondInteraction(stringArg(args, "interaction_id", ""), stringArg(args, "option_id", ""), false)
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "interaction": interaction, "responded": true}, nil
	case "cancel":
		interaction, err := s.manager.RespondInteraction(stringArg(args, "interaction_id", ""), "", true)
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "interaction": interaction, "cancelled": true}, nil
	default:
		return nil, validationError("ACP_ACTION_INVALID", "unsupported ACP interaction action", map[string]any{"action": action})
	}
}
