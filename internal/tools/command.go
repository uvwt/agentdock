package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/uvwt/agentdock/internal/policy"
	"github.com/uvwt/agentdock/internal/sandbox"
	"github.com/uvwt/agentdock/internal/session"
)

type SessionStore = session.Store

func NewSessionStore() *SessionStore { return session.NewStore() }

func (r *Runtime) execCommand(ctx context.Context, args map[string]any) (Result, error) {
	cmd := stringArg(args, "cmd", "")
	if cmd == "" {
		return nil, toolError("INVALID_ARGUMENT", "cmd is required", "validation")
	}
	decision := policy.CheckCommand(cmd, r.cfg.DangerouslySkipAllPermissions)
	if !decision.Allowed {
		return nil, toolErrorDetails("PERMISSION_REQUIRED", decision.Reason, "permission", map[string]any{"permission": decision.Permission, "command": decision.Command})
	}
	workdir, err := r.ws.ResolveExisting(stringArg(args, "workdir", "."))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(workdir.Abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, toolError("NOT_A_DIRECTORY", "workdir is not a directory", "validation")
	}
	timeout := time.Duration(intArg(args, "timeout_ms", 30000)) * time.Millisecond
	yield := time.Duration(intArg(args, "yield_time_ms", 1000)) * time.Millisecond
	if yield > 30*time.Second {
		yield = 30 * time.Second
	}
	maxBytes := intArg(args, "max_output_bytes", 65536)
	tty := boolArg(args, "tty", false)

	// 这里故意不用请求 ctx 派生子进程生命周期。
	// 背景：exec_command 可能先返回 running，让模型后续通过 session_status 继续取结果；
	// 如果子进程绑定到单次 MCP 请求 ctx，请求结束时 git push / npm install 等长任务会被杀掉。
	// 因此长任务只受 timeout_ms 和 kill_session / kill_all_sessions 控制。
	s, sandboxStatus, err := session.Start(context.Background(), cmd, workdir.Abs, r.commandEnv(mapArg(args, "env")), timeout, func(command *exec.Cmd) (func(), map[string]any) {
		if r.cfg.SandboxMode == "none" {
			// 裸机可信部署需要 sudo 时，不能启用 Landlock；Landlock 必须设置 no_new_privs，
			// 会导致 sudo 无法提权。默认仍是 landlock，只有显式 sandbox-mode=none 才跳过。
			return func() {}, map[string]any{"enabled": false, "mode": "none", "warnings": []string{"command sandbox disabled by configuration; rely on OS user permissions and sudoers policy"}}
		}
		cleanup, status := sandbox.PrepareCommand(command, r.ws.Root())
		return cleanup, map[string]any{"enabled": status.Enabled, "mode": "landlock", "warnings": status.Warnings}
	})
	if err != nil {
		return nil, err
	}
	if stdin := stringArg(args, "stdin", ""); stdin != "" {
		_ = s.Write(stdin)
	}
	if !tty {
		s.CloseStdin()
	}

	select {
	case err := <-s.Done:
		s.Cancel()
		result := s.Snapshot("exited", maxBytes)
		result["sandbox"] = sandboxStatus
		if s.TimedOut {
			result["status"] = "timeout"
		}
		if err != nil && !s.TimedOut {
			result["error"] = err.Error()
		}
		addCommandDiagnostics(result)
		return result, nil
	case <-time.After(yield):
		if boolArg(args, "wait_until_exit", false) {
			select {
			case err := <-s.Done:
				s.Cancel()
				result := s.Snapshot("exited", maxBytes)
				result["sandbox"] = sandboxStatus
				if s.TimedOut {
					result["status"] = "timeout"
				}
				if err != nil && !s.TimedOut {
					result["error"] = err.Error()
				}
				addCommandDiagnostics(result)
				return result, nil
			case <-ctx.Done():
				r.sessions.Add(s)
				result := s.Snapshot("running", maxBytes)
				result["sandbox"] = sandboxStatus
				return result, nil
			}
		}
		r.sessions.Add(s)
		result := s.Snapshot("running", maxBytes)
		result["sandbox"] = sandboxStatus
		return result, nil
	}
}

func (r *Runtime) writeStdin(args map[string]any) (Result, error) {
	s, ok := r.sessions.Get(stringArg(args, "session_id", ""))
	if !ok {
		return nil, toolError("SESSION_NOT_FOUND", "session not found", "not_found")
	}
	if chars := stringArg(args, "chars", ""); chars != "" {
		if err := s.Write(chars); err != nil && err != io.ErrClosedPipe {
			return nil, err
		}
	}
	select {
	case err := <-s.Done:
		s.Cancel()
		r.sessions.Delete(s.ID)
		result := s.Snapshot("exited", intArg(args, "max_output_bytes", 65536))
		if err != nil && !s.TimedOut {
			result["error"] = err.Error()
		}
		return result, nil
	default:
		return s.Snapshot("running", intArg(args, "max_output_bytes", 65536)), nil
	}
}

func (r *Runtime) killSession(args map[string]any) (Result, error) {
	started := time.Now()
	s, ok := r.sessions.Get(stringArg(args, "session_id", ""))
	if !ok {
		return nil, toolError("SESSION_NOT_FOUND", "session not found", "not_found")
	}
	s.Kill()
	r.sessions.Delete(s.ID)
	result := s.Snapshot("killed", intArg(args, "max_output_bytes", 65536))
	result["kill_operation_ms"] = time.Since(started).Milliseconds()
	return result, nil
}

func (r *Runtime) killAllSessions(args map[string]any) (Result, error) {
	items := make([]map[string]any, 0)
	for _, s := range r.sessions.List() {
		s.Kill()
		r.sessions.Delete(s.ID)
		items = append(items, map[string]any{"session_id": s.ID, "status": "killed"})
	}
	return Result{"ok": true, "sessions": items, "count": len(items)}, nil
}

func (r *Runtime) sessionStatus(args map[string]any) (Result, error) {
	s, ok := r.sessions.Get(stringArg(args, "session_id", ""))
	if !ok {
		return nil, toolError("SESSION_NOT_FOUND", "session not found", "not_found")
	}
	select {
	case err := <-s.Done:
		s.Cancel()
		r.sessions.Delete(s.ID)
		result := s.Snapshot("exited", intArg(args, "max_output_bytes", 65536))
		if s.TimedOut {
			result["status"] = "timeout"
		}
		if err != nil && !s.TimedOut {
			result["error"] = err.Error()
		}
		addCommandDiagnostics(result)
		return result, nil
	default:
		return s.Snapshot("running", intArg(args, "max_output_bytes", 65536)), nil
	}
}

func (r *Runtime) listSessions() (Result, error) {
	items := make([]map[string]any, 0)
	for _, s := range r.sessions.List() {
		select {
		case <-s.Done:
			r.sessions.Delete(s.ID)
			continue
		default:
		}
		summary := s.Summary("running")
		items = append(items, map[string]any{"session_id": summary.ID, "status": summary.Status, "elapsed_ms": summary.ElapsedMS, "timed_out": summary.TimedOut})
	}
	return Result{"ok": true, "sessions": items, "count": len(items)}, nil
}

func addCommandDiagnostics(result Result) {
	combined := fmt.Sprint(result["stdout"]) + "\n" + fmt.Sprint(result["stderr"])
	if diag := diagnoseGitOutput(combined); diag != nil {
		result["diagnostic"] = diag
	}
}

func redactCommandResult(result Result, patterns []string) {
	for _, key := range []string{"stdout", "stderr", "error"} {
		if value, ok := result[key].(string); ok {
			result[key] = redactSecrets(value, patterns)
		}
	}
}

func (r *Runtime) commandEnv(extra map[string]any) []string {
	env := map[string]string{}
	for _, key := range []string{"PATH", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}
	env["HOME"] = r.ws.Root()
	env["TMPDIR"] = filepath.Join(r.ws.Root(), ".tmp")
	_ = os.MkdirAll(env["TMPDIR"], 0o755)
	if r.cfg.DangerouslySkipAllPermissions {
		for key, value := range extra {
			env[key] = fmt.Sprint(value)
		}
	}
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}
