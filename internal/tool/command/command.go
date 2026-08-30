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

func (svc *Service) Exec(ctx context.Context, request ExecRequest) (Result, error) {
	if request.Cmd == "" {
		return nil, toolError("INVALID_ARGUMENT", "cmd is required", "validation")
	}
	invocation, err := svc.prepareCommandInvocation(request)
	if err != nil {
		return nil, err
	}
	timeout, err := commandTimeout(request.TimeoutMS)
	if err != nil {
		return nil, err
	}
	executionMode, err := commandExecutionModeArg(request.ExecutionMode)
	if err != nil {
		return nil, err
	}
	defaultYieldMS := int(defaultCommandYield / time.Millisecond)
	yieldMS := boundedInt(intValue(request.YieldTimeMS, defaultYieldMS), defaultYieldMS, 0, int(maxCommandYield/time.Millisecond))
	yield := time.Duration(yieldMS) * time.Millisecond
	maxBytes := commandOutputLimit(request.MaxOutputBytes)
	tty := request.TTY
	commandCtx, err := svc.commandContext()
	if err != nil {
		return nil, err
	}

	if !svc.sessions.TryReserve(maxConcurrentCommandSessions) {
		if svc.sessions.Closing() {
			return nil, toolError("RUNTIME_CLOSING", "AgentDock runtime is shutting down", "runtime")
		}
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
	s, sandboxStatus, err := invocation.start(commandCtx, timeout, tty, func(command *exec.Cmd) (func(), session.PreparationStatus) {
		// AgentDock 不额外过滤命令，实际权限边界由所选运行环境决定。
		privilegeWarning := "exec_command runs with the AgentDock process OS user privileges"
		if invocation.execution.Runtime == "wsl" {
			privilegeWarning = "runtime=wsl executes with the selected distribution's default Linux user privileges"
		}
		return func() {}, session.PreparationStatus{Enabled: false, Mode: "none", Policy: "no_command_content_filtering", Warnings: []string{privilegeWarning, "use Docker volumes, service users, file permissions, and network policy as the security boundary"}}
	})
	// 只有 invocation.start 完整返回后，runner、平台进程控制器以及 cmdCtx 取消监听才都已经建立。
	// Runtime.Close 会等待这个启动窗口排空，再取消 commandCtx，避免在半启动状态抢占进程。
	svc.sessions.FinishStart()
	if err != nil {
		return nil, err
	}
	s.SetExecutionContext(invocation.execution)
	if request.Stdin != "" {
		if err := s.Write(request.Stdin); err != nil {
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
		result := snapshotResult(s.Snapshot("running", maxBytes))
		result["sandbox"] = preparationStatusResult(sandboxStatus)
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
	result := snapshotResult(s.Snapshot("exited", maxBytes))
	result["sandbox"] = preparationStatusResult(sandboxStatus)
	if s.TimedOut {
		result["status"] = "timeout"
	}
	if err != nil {
		result["command_error"] = err.Error()
	}
	return result, nil
}

func commandExecutionModeArg(raw string) (commandExecutionMode, error) {
	if raw == "" {
		raw = string(commandExecutionModeAuto)
	}
	mode := commandExecutionMode(raw)
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

func commandTimeout(requested *int) (time.Duration, error) {
	timeoutMS := intValue(requested, 30000)
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

func commandOutputLimit(requested *int) int {
	return boundedInt(intValue(requested, 65536), 65536, 1, maxCommandOutputBytes)
}

// snapshotResult 是 Command capability 向外部 Tool Result 的输出边界；Session 核心只暴露强类型快照。
func snapshotResult(snapshot session.Snapshot) Result {
	result := Result{
		"session_id": snapshot.SessionID, "status": snapshot.Status,
		"stdout": snapshot.Stdout, "stderr": snapshot.Stderr,
		"elapsed_ms": snapshot.ElapsedMS, "timed_out": snapshot.TimedOut, "terminal": snapshot.Terminal,
		"stdout_output_bytes": snapshot.StdoutOutputBytes, "stderr_output_bytes": snapshot.StderrOutputBytes,
		"stdout_total_bytes": snapshot.StdoutTotalBytes, "stderr_total_bytes": snapshot.StderrTotalBytes,
		"stdout_dropped_bytes": snapshot.StdoutDroppedBytes, "stderr_dropped_bytes": snapshot.StderrDroppedBytes,
		"stdout_omitted_bytes": snapshot.StdoutOmittedBytes, "stderr_omitted_bytes": snapshot.StderrOmittedBytes,
		"stdout_output_lines": snapshot.StdoutOutputLines, "stderr_output_lines": snapshot.StderrOutputLines,
		"stdout_truncated": snapshot.StdoutTruncated, "stderr_truncated": snapshot.StderrTruncated,
	}
	if snapshot.Completed {
		result["exit_code"] = snapshot.ExitCode
		result["command_ok"] = snapshot.CommandOK
	}
	if snapshot.Runtime != "" {
		result["runtime"] = snapshot.Runtime
	}
	if snapshot.WSLDistribution != "" {
		result["wsl_distribution"] = snapshot.WSLDistribution
	}
	if snapshot.Workdir != "" {
		result["workdir"] = snapshot.Workdir
	}
	return result
}

func preparationStatusResult(status session.PreparationStatus) map[string]any {
	result := map[string]any{"enabled": status.Enabled}
	if status.Mode != "" {
		result["mode"] = status.Mode
	}
	if status.Policy != "" {
		result["policy"] = status.Policy
	}
	if len(status.Warnings) > 0 {
		result["warnings"] = append([]string(nil), status.Warnings...)
	}
	return result
}

func (svc *Service) writeStdin(request SessionActRequest) (Result, error) {
	s, ok := svc.sessions.Get(request.SessionID)
	if !ok {
		return nil, toolError("SESSION_NOT_FOUND", "session not found", "not_found")
	}
	maxBytes := commandOutputLimit(request.MaxOutputBytes)
	select {
	case <-s.Done:
		return svc.consumeCompletedSession(s, maxBytes), nil
	default:
	}

	if request.Chars != "" {
		if err := s.Write(request.Chars); err != nil {
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
		return snapshotResult(s.Peek("running", maxBytes)), nil
	}
}

func (svc *Service) consumeCompletedSession(s *session.Session, maxBytes int) Result {
	err := s.WaitError()
	s.Cancel()
	svc.sessions.Delete(s.ID)
	result := snapshotResult(s.Snapshot("exited", maxBytes))
	if s.TimedOut {
		result["status"] = "timeout"
	}
	if err != nil {
		result["command_error"] = err.Error()
	}
	return result
}

func (svc *Service) killSession(request SessionActRequest) (Result, error) {
	started := time.Now()
	s, ok := svc.sessions.Get(request.SessionID)
	if !ok {
		return nil, toolError("SESSION_NOT_FOUND", "session not found", "not_found")
	}
	select {
	case <-s.Done:
		return svc.consumeCompletedSession(s, commandOutputLimit(request.MaxOutputBytes)), nil
	default:
	}
	_, killErr := s.Kill()
	if killErr != nil {
		return nil, toolErrorDetails(
			"SESSION_KILL_FAILED",
			"failed to terminate the session process tree",
			"runtime",
			map[string]any{"session_id": s.ID, "reason": killErr.Error()},
		)
	}
	if !waitForSessionCompletion(s, sessionKillWait) {
		return nil, toolErrorDetails(
			"SESSION_KILL_TIMEOUT",
			"session did not stop after kill request",
			"runtime",
			map[string]any{"session_id": s.ID, "wait_ms": sessionKillWait.Milliseconds()},
		)
	}
	svc.sessions.Delete(s.ID)
	result := snapshotResult(s.Snapshot("killed", commandOutputLimit(request.MaxOutputBytes)))
	if err := s.WaitError(); err != nil {
		result["command_error"] = err.Error()
	}
	result["kill_operation_ms"] = time.Since(started).Milliseconds()
	return result, nil
}

func (svc *Service) killAll() (Result, error) {
	sessions := svc.sessions.List()
	running := make([]*session.Session, 0, len(sessions))
	items := make([]map[string]any, 0, len(sessions))
	killFailures := make([]map[string]any, 0)
	for _, s := range sessions {
		select {
		case <-s.Done:
			summary := s.Summary()
			s.Cancel()
			svc.sessions.Delete(s.ID)
			items = append(items, map[string]any{"session_id": s.ID, "status": summary.Status})
		default:
			_, killErr := s.Kill()
			if killErr != nil {
				killFailures = append(killFailures, map[string]any{"session_id": s.ID, "reason": killErr.Error()})
			}
			running = append(running, s)
		}
	}
	completed, timedOut := waitForSessionsCompletion(running, sessionKillWait)
	for _, s := range completed {
		svc.sessions.Delete(s.ID)
		items = append(items, map[string]any{"session_id": s.ID, "status": "killed"})
	}
	if len(killFailures) > 0 {
		details := map[string]any{"sessions": killFailures}
		if len(timedOut) > 0 {
			details["timed_out_session_ids"] = timedOut
			details["wait_ms"] = sessionKillWait.Milliseconds()
		}
		return nil, toolErrorDetails(
			"SESSION_KILL_FAILED",
			"failed to terminate one or more session process trees",
			"runtime",
			details,
		)
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

func (svc *Service) sessionStatus(request SessionObserveRequest) (Result, error) {
	s, ok := svc.sessions.Get(request.SessionID)
	if !ok {
		return nil, toolError("SESSION_NOT_FOUND", "session not found", "not_found")
	}
	maxBytes := commandOutputLimit(request.MaxOutputBytes)
	select {
	case <-s.Done:
		return svc.consumeCompletedSession(s, maxBytes), nil
	default:
		return snapshotResult(s.Snapshot("running", maxBytes)), nil
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

func (svc *Service) commandEnv(skillName string, extra map[string]string) ([]string, error) {
	env, err := svc.baseCommandEnv()
	if err != nil {
		return nil, err
	}
	overrides, err := svc.commandEnvOverrides(skillName, extra)
	if err != nil {
		return nil, err
	}
	for key, value := range overrides {
		setPlatformCommandEnv(env, key, value)
	}
	return formatCommandEnv(env), nil
}

func (svc *Service) internalCommandEnv(extra map[string]string) ([]string, error) {
	env, err := svc.baseCommandEnv()
	if err != nil {
		return nil, err
	}
	for key, value := range extra {
		setPlatformCommandEnv(env, key, value)
	}
	return formatCommandEnv(env), nil
}

func (svc *Service) baseCommandEnv() (map[string]string, error) {
	env := map[string]string{}
	keys := append([]string{"PATH", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR", "TEMP", "TMP"}, platformCommandEnvKeys()...)
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}
	repairPlatformCommandEnv(env)
	env["AGENTDOCK_HOME"] = svc.config().AgentDockHome
	env["AGENTDOCK_DEFAULT_DIR"] = svc.config().AgentDockDefaultDir
	hostHome := ""
	if resolvedHome, err := os.UserHomeDir(); err == nil && resolvedHome != "" {
		hostHome = resolvedHome
		env["HOME"] = resolvedHome
	}
	// macOS 的 launchd 默认只提供系统 PATH。命令工具需要补齐常见用户级可执行目录，
	// 否则 Homebrew、~/.local/bin 中已安装的 CLI 在桌面服务里会表现为“未安装”。
	if commandPath := platformCommandPath(env["PATH"], hostHome); commandPath != "" {
		env["PATH"] = commandPath
	}
	commandTempDir := filepath.Join(svc.config().AgentDockHome, "tmp")
	env["TMPDIR"] = commandTempDir
	configurePlatformCommandTempEnv(env, commandTempDir)
	if err := os.MkdirAll(commandTempDir, 0o755); err != nil {
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
