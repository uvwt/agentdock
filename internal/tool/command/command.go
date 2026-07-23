package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/uvwt/agentdock/internal/tool/command/session"
)

type SessionStore = session.Store

type commandExecutionMode string

const (
	commandExecutionModeAuto  commandExecutionMode = "auto"
	commandExecutionModeSync  commandExecutionMode = "sync"
	commandExecutionModeAsync commandExecutionMode = "async"

	completedSessionRetention    = time.Hour
	sessionKillWait              = 3 * time.Second
	defaultCommandYield          = 5 * time.Second
	maxCommandYield              = 30 * time.Second
	maxCommandTimeout            = 24 * time.Hour
	maxCommandOutputBytes        = 4 << 20
	maxConcurrentCommandSessions = 32
	maxRetainedCommandSessions   = 128
)

func NewSessionStore() *SessionStore { return session.NewStore() }

func (svc *Service) Exec(ctx context.Context, args map[string]any) (Result, error) {
	cmd := stringArg(args, "cmd", "")
	if cmd == "" {
		return nil, toolError("INVALID_ARGUMENT", "cmd is required", "validation")
	}
	invocation, err := svc.prepareCommandInvocation(args, cmd)
	if err != nil {
		return nil, err
	}
	timeout, err := commandTimeout(args)
	if err != nil {
		return nil, err
	}
	executionMode, err := commandExecutionModeArg(args)
	if err != nil {
		return nil, err
	}
	defaultYieldMS := int(defaultCommandYield / time.Millisecond)
	yieldMS := boundedInt(intArg(args, "yield_time_ms", defaultYieldMS), defaultYieldMS, 0, int(maxCommandYield/time.Millisecond))
	yield := time.Duration(yieldMS) * time.Millisecond
	maxBytes := commandOutputLimit(args)
	tty := boolArg(args, "tty", false)
	commandCtx, err := svc.commandContext()
	if err != nil {
		return nil, err
	}

	if !svc.sessions.TryReserve(maxConcurrentCommandSessions) {
		return nil, toolErrorDetails(
			"SESSION_LIMIT_REACHED",
			"too many command sessions are already running",
			"resource_limit",
			map[string]any{"max_running_sessions": maxConcurrentCommandSessions},
		)
	}
	reservationActive := true
	defer func() {
		if reservationActive {
			svc.sessions.ReleaseReservation()
		}
	}()

	// 这里故意不用请求 ctx 派生子进程生命周期。
	// 背景：exec_command 可能先返回 running，让模型后续通过 session_observe action=status 继续取结果；
	// 如果子进程绑定到单次 MCP 请求 ctx，请求结束时 git push / npm install 等长任务会被杀掉。
	// 因此长任务只受 timeout_ms 和 session_act action=kill/kill_all 控制。
	s, sandboxStatus, err := invocation.start(commandCtx, timeout, tty, func(command *exec.Cmd) (func(), map[string]any) {
		// AgentDock 不额外过滤命令，实际权限边界由所选运行环境决定。
		privilegeWarning := "exec_command runs with the AgentDock process OS user privileges"
		if invocation.execution.Runtime == "wsl" {
			privilegeWarning = "runtime=wsl executes with the selected distribution's default Linux user privileges"
		}
		return func() {}, map[string]any{"enabled": false, "mode": "none", "policy": "no_command_content_filtering", "warnings": []string{privilegeWarning, "use Docker volumes, service users, file permissions, and network policy as the security boundary"}}
	})
	if err != nil {
		return nil, err
	}
	s.SetExecutionContext(invocation.execution)
	if stdin := stringArg(args, "stdin", ""); stdin != "" {
		if err := s.Write(stdin); err != nil {
			s.Kill()
			s.Cancel()
			return nil, fmt.Errorf("write command stdin: %w", err)
		}
	}
	if !tty {
		if err := s.CloseStdin(); err != nil && !errors.Is(err, os.ErrClosed) {
			s.Kill()
			s.Cancel()
			return nil, fmt.Errorf("close command stdin: %w", err)
		}
	}

	storeSession := func(reason string) Result {
		svc.storeReservedSession(s)
		reservationActive = false
		result := s.Snapshot("running", maxBytes)
		result["sandbox"] = sandboxStatus
		result["session_reason"] = reason
		result["observe_after_ms"] = 1000
		return result
	}

	switch executionMode {
	case commandExecutionModeAsync:
		return storeSession("explicit_async"), nil
	case commandExecutionModeSync:
		select {
		case <-s.Done:
		case <-ctx.Done():
			return storeSession("request_cancelled"), nil
		}
	case commandExecutionModeAuto:
		timer := time.NewTimer(yield)
		defer timer.Stop()
		select {
		case <-s.Done:
		case <-timer.C:
			return storeSession("foreground_threshold_exceeded"), nil
		case <-ctx.Done():
			return storeSession("request_cancelled"), nil
		}
	}

	err = s.WaitError()
	s.Cancel()
	result := s.Snapshot("exited", maxBytes)
	result["sandbox"] = sandboxStatus
	if s.TimedOut {
		result["status"] = "timeout"
	}
	if err != nil {
		result["command_error"] = err.Error()
	}
	svc.addCommandDiagnostics(result)
	return result, nil
}

func commandExecutionModeArg(args map[string]any) (commandExecutionMode, error) {
	mode := commandExecutionMode(stringArg(args, "execution_mode", string(commandExecutionModeAuto)))
	switch mode {
	case commandExecutionModeAuto, commandExecutionModeSync, commandExecutionModeAsync:
		return mode, nil
	default:
		return "", toolErrorDetails(
			"INVALID_EXECUTION_MODE",
			"execution_mode must be auto, sync, or async",
			"validation",
			map[string]any{"execution_mode": mode, "allowed": []string{"auto", "sync", "async"}},
		)
	}
}

func commandTimeout(args map[string]any) (time.Duration, error) {
	timeoutMS := intArg(args, "timeout_ms", 30000)
	if timeoutMS <= 0 {
		return 0, toolErrorDetails(
			"INVALID_TIMEOUT",
			"timeout_ms must be a positive integer",
			"validation",
			map[string]any{"timeout_ms": timeoutMS},
		)
	}
	maximumMS := int(maxCommandTimeout / time.Millisecond)
	if timeoutMS > maximumMS {
		timeoutMS = maximumMS
	}
	return time.Duration(timeoutMS) * time.Millisecond, nil
}

func commandOutputLimit(args map[string]any) int {
	return boundedInt(intArg(args, "max_output_bytes", 65536), 65536, 1, maxCommandOutputBytes)
}

func (svc *Service) writeStdin(args map[string]any) (Result, error) {
	s, ok := svc.sessions.Get(stringArg(args, "session_id", ""))
	if !ok {
		return nil, toolError("SESSION_NOT_FOUND", "session not found", "not_found")
	}
	maxBytes := commandOutputLimit(args)
	select {
	case <-s.Done:
		return svc.consumeCompletedSession(s, maxBytes), nil
	default:
	}

	if chars := stringArg(args, "chars", ""); chars != "" {
		if err := s.Write(chars); err != nil {
			select {
			case <-s.Done:
				return svc.consumeCompletedSession(s, maxBytes), nil
			default:
			}
			if !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, os.ErrClosed) {
				return nil, fmt.Errorf("write session stdin: %w", err)
			}
		}
	}
	select {
	case <-s.Done:
		return svc.consumeCompletedSession(s, maxBytes), nil
	default:
		return s.Peek("running", maxBytes), nil
	}
}

func (svc *Service) consumeCompletedSession(s *session.Session, maxBytes int) Result {
	err := s.WaitError()
	s.Cancel()
	svc.sessions.Delete(s.ID)
	result := s.Snapshot("exited", maxBytes)
	if s.TimedOut {
		result["status"] = "timeout"
	}
	if err != nil {
		result["command_error"] = err.Error()
	}
	svc.addCommandDiagnostics(result)
	return result
}

func (svc *Service) killSession(args map[string]any) (Result, error) {
	started := time.Now()
	s, ok := svc.sessions.Get(stringArg(args, "session_id", ""))
	if !ok {
		return nil, toolError("SESSION_NOT_FOUND", "session not found", "not_found")
	}
	select {
	case <-s.Done:
		return svc.consumeCompletedSession(s, commandOutputLimit(args)), nil
	default:
	}
	s.Kill()
	if !waitForSessionCompletion(s, sessionKillWait) {
		return nil, toolErrorDetails(
			"SESSION_KILL_TIMEOUT",
			"session did not stop after kill request",
			"runtime",
			map[string]any{"session_id": s.ID, "wait_ms": sessionKillWait.Milliseconds()},
		)
	}
	svc.sessions.Delete(s.ID)
	result := s.Snapshot("killed", commandOutputLimit(args))
	if err := s.WaitError(); err != nil {
		result["command_error"] = err.Error()
	}
	result["kill_operation_ms"] = time.Since(started).Milliseconds()
	svc.addCommandDiagnostics(result)
	return result, nil
}

func (svc *Service) KillAll(args map[string]any) (Result, error) {
	sessions := svc.sessions.List()
	running := make([]*session.Session, 0, len(sessions))
	items := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		select {
		case <-s.Done:
			summary := s.Summary()
			s.Cancel()
			svc.sessions.Delete(s.ID)
			items = append(items, map[string]any{"session_id": s.ID, "status": summary.Status})
		default:
			s.Kill()
			running = append(running, s)
		}
	}

	completed, timedOut := waitForSessionsCompletion(running, sessionKillWait)
	for _, s := range completed {
		svc.sessions.Delete(s.ID)
		items = append(items, map[string]any{"session_id": s.ID, "status": "killed"})
	}
	if len(timedOut) > 0 {
		return nil, toolErrorDetails(
			"SESSION_KILL_TIMEOUT",
			"one or more sessions did not stop after kill request",
			"runtime",
			map[string]any{"session_ids": timedOut, "wait_ms": sessionKillWait.Milliseconds()},
		)
	}
	return Result{"sessions": items, "count": len(items)}, nil
}

func waitForSessionCompletion(s *session.Session, timeout time.Duration) bool {
	if timeout <= 0 {
		select {
		case <-s.Done:
			return true
		default:
			return false
		}
	}
	select {
	case <-s.Done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func waitForSessionsCompletion(sessions []*session.Session, timeout time.Duration) ([]*session.Session, []string) {
	deadline := time.Now().Add(timeout)
	completed := make([]*session.Session, 0, len(sessions))
	timedOut := make([]string, 0)
	for _, s := range sessions {
		remaining := time.Until(deadline)
		if waitForSessionCompletion(s, remaining) {
			completed = append(completed, s)
			continue
		}
		timedOut = append(timedOut, s.ID)
	}
	return completed, timedOut
}

func (svc *Service) sessionStatus(args map[string]any) (Result, error) {
	s, ok := svc.sessions.Get(stringArg(args, "session_id", ""))
	if !ok {
		return nil, toolError("SESSION_NOT_FOUND", "session not found", "not_found")
	}
	maxBytes := commandOutputLimit(args)
	select {
	case <-s.Done:
		return svc.consumeCompletedSession(s, maxBytes), nil
	default:
		return s.Snapshot("running", maxBytes), nil
	}
}

func (svc *Service) storeReservedSession(s *session.Session) {
	svc.sessions.PruneCompletedBefore(time.Now().Add(-completedSessionRetention))
	svc.sessions.AddReserved(s)
	svc.sessions.PruneCompletedToLimit(maxRetainedCommandSessions)
}

func (svc *Service) listSessions() (Result, error) {
	// list 是只读观察入口，不能消费刚完成命令的最终输出。完成结果保留一小时，
	// 由 status 正常读取后删除；无人领取的旧结果再在这里统一淘汰。
	svc.sessions.PruneCompletedBefore(time.Now().Add(-completedSessionRetention))
	items := make([]map[string]any, 0)
	for _, s := range svc.sessions.List() {
		summary := s.Summary()
		item := map[string]any{"session_id": summary.ID, "status": summary.Status, "elapsed_ms": summary.ElapsedMS, "timed_out": summary.TimedOut}
		if summary.Runtime != "" {
			item["runtime"] = summary.Runtime
		}
		if summary.Distribution != "" {
			item["wsl_distribution"] = summary.Distribution
		}
		if summary.Workdir != "" {
			item["workdir"] = summary.Workdir
		}
		items = append(items, item)
	}
	return Result{"sessions": items, "count": len(items)}, nil
}

func (svc *Service) addCommandDiagnostics(result Result) {
	if svc.diagnose == nil {
		return
	}
	combined := fmt.Sprint(result["stdout"]) + "\n" + fmt.Sprint(result["stderr"])
	if diagnostic := svc.diagnose(combined); diagnostic != nil {
		result["diagnostic"] = diagnostic
	}
}

func (svc *Service) commandEnv(skillName string, extra map[string]any) ([]string, error) {
	env, err := svc.baseCommandEnv()
	if err != nil {
		return nil, err
	}
	overrides, err := svc.commandEnvOverrides(skillName, extra)
	if err != nil {
		return nil, err
	}
	for key, value := range overrides {
		env[key] = value
	}
	return formatCommandEnv(env), nil
}

func (svc *Service) internalCommandEnv(extra map[string]string) ([]string, error) {
	env, err := svc.baseCommandEnv()
	if err != nil {
		return nil, err
	}
	for key, value := range extra {
		env[key] = value
	}
	return formatCommandEnv(env), nil
}

func (svc *Service) baseCommandEnv() (map[string]string, error) {
	env := map[string]string{}
	for _, key := range []string{"PATH", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP", "WSLENV"} {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}
	env["AGENTDOCK_HOME"] = svc.config().AgentDockHome
	env["AGENTDOCK_DEFAULT_DIR"] = svc.config().AgentDockDefaultDir
	if hostHome, err := os.UserHomeDir(); err == nil && hostHome != "" {
		env["HOME"] = hostHome
	}
	env["TMPDIR"] = filepath.Join(svc.config().AgentDockHome, "tmp")
	if err := os.MkdirAll(env["TMPDIR"], 0o755); err != nil {
		return nil, fmt.Errorf("create command temp directory: %w", err)
	}
	return env, nil
}

func formatCommandEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}
