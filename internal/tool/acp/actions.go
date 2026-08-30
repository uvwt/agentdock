package acp

import (
	"context"
	"time"

	acpruntime "github.com/uvwt/agentdock/internal/acp"
)

func (s *Service) Session(ctx context.Context, request SessionRequest) (Result, error) {
	if s == nil || s.manager == nil {
		return nil, validationError("ACP_NOT_CONFIGURED", "ACP runtime is not configured", nil)
	}
	action := actionArg(request.Action)
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
		methodID := request.AuthMethodID
		if err := s.manager.Authenticate(ctx, methodID); err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "auth_method_id": methodID, "authenticated": true}, nil
	case "new":
		result, err := s.manager.NewSession(ctx, request.CWD, request.AdditionalDirectories)
		if err != nil {
			return nil, acpToolError(err)
		}
		return sessionActionResult(action, result), nil
	case "load":
		result, err := s.manager.LoadSession(ctx, request.SessionID)
		if err != nil {
			return nil, acpToolError(err)
		}
		return sessionActionResult(action, result), nil
	case "resume":
		result, err := s.manager.ResumeSession(ctx, request.SessionID)
		if err != nil {
			return nil, acpToolError(err)
		}
		return sessionActionResult(action, result), nil
	case "fork":
		var additional []string
		if request.AdditionalDirectories != nil {
			additional = append([]string{}, request.AdditionalDirectories...)
		}
		result, err := s.manager.ForkSession(ctx, request.SessionID, request.CWD, additional)
		if err != nil {
			return nil, acpToolError(err)
		}
		return sessionActionResult(action, result), nil
	case "set_mode":
		sessionID := request.SessionID
		modeID := request.ModeID
		if err := s.manager.SetSessionMode(ctx, sessionID, modeID); err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "session_id": sessionID, "mode_id": modeID, "changed": true}, nil
	case "set_config":
		sessionID := request.SessionID
		configID := request.ConfigID
		configOptions, err := s.manager.SetSessionConfigOption(ctx, sessionID, configID, request.ConfigValue)
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
		session, err := s.manager.InspectSession(request.SessionID)
		if err != nil {
			return nil, acpToolError(err)
		}
		messages, err := s.manager.SessionMessages(session.ID)
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "session": session, "messages": messages}, nil
	case "close":
		session, err := s.manager.CloseSession(ctx, request.SessionID)
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "session": session}, nil
	case "delete":
		sessionID := request.SessionID
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

func (s *Service) Prompt(ctx context.Context, request PromptRequest) (Result, error) {
	if s == nil || s.manager == nil {
		return nil, validationError("ACP_NOT_CONFIGURED", "ACP runtime is not configured", nil)
	}
	action := actionArg(request.Action)
	switch action {
	case "start":
		result, err := s.manager.StartPrompt(ctx, request.SessionID, request.Text)
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{
			"action": action, "run_id": result.RunID, "session_id": result.SessionID,
			"status": result.Status, "started_at": result.StartedAt,
		}, nil
	case "events":
		after := intValue(request.AfterSeq, 0)
		if after < 0 {
			return nil, validationError("ACP_AFTER_SEQ_INVALID", "after_seq must not be negative", map[string]any{"after_seq": after})
		}
		limit := intValue(request.Limit, 100)
		waitMS := intValue(request.WaitMS, 0)
		if waitMS < 0 {
			return nil, validationError("ACP_WAIT_INVALID", "wait_ms must not be negative", map[string]any{"wait_ms": waitMS})
		}
		if waitMS > 25000 {
			waitMS = 25000
		}
		result, err := s.manager.PromptEvents(ctx, request.RunID, uint64(after), limit, time.Duration(waitMS)*time.Millisecond)
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
		result, err := s.manager.Steer(ctx, request.SessionID, request.Text)
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "session_id": request.SessionID, "steering": result}, nil
	case "cancel":
		sessionID := request.SessionID
		runID := request.RunID
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

func (s *Service) Interaction(_ context.Context, request InteractionRequest) (Result, error) {
	if s == nil || s.manager == nil {
		return nil, validationError("ACP_NOT_CONFIGURED", "ACP runtime is not configured", nil)
	}
	action := actionArg(request.Action)
	switch action {
	case "list":
		interactions := s.manager.ListInteractions(request.SessionID, boolValue(request.PendingOnly, true))
		return Result{"action": action, "interactions": interactions, "count": len(interactions)}, nil
	case "inspect":
		interaction, err := s.manager.InspectInteraction(request.InteractionID)
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "interaction": interaction}, nil
	case "respond":
		interaction, err := s.manager.RespondInteraction(request.InteractionID, request.OptionID, false)
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "interaction": interaction, "responded": true}, nil
	case "cancel":
		interaction, err := s.manager.RespondInteraction(request.InteractionID, "", true)
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{"action": action, "interaction": interaction, "cancelled": true}, nil
	default:
		return nil, validationError("ACP_ACTION_INVALID", "unsupported ACP interaction action", map[string]any{"action": action})
	}
}
