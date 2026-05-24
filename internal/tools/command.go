package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/local/coding-tools-mcp-go/internal/policy"
	"github.com/local/coding-tools-mcp-go/internal/sandbox"
	"github.com/local/coding-tools-mcp-go/internal/session"
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

	s, sandboxStatus, err := session.Start(ctx, cmd, workdir.Abs, r.commandEnv(mapArg(args, "env")), timeout, func(command *exec.Cmd) (func(), map[string]any) {
		cleanup, status := sandbox.PrepareCommand(command, r.ws.Root())
		return cleanup, map[string]any{"enabled": status.Enabled, "warnings": status.Warnings}
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
		if s.TimedOut { result["status"] = "timeout" }
		if err != nil && !s.TimedOut { result["error"] = err.Error() }
		return result, nil
	case <-time.After(yield):
		r.sessions.Add(s)
		result := s.Snapshot("running", maxBytes)
		result["sandbox"] = sandboxStatus
		return result, nil
	}
}

func (r *Runtime) writeStdin(args map[string]any) (Result, error) {
	s, ok := r.sessions.Get(stringArg(args, "session_id", ""))
	if !ok { return nil, toolError("SESSION_NOT_FOUND", "session not found", "not_found") }
	if chars := stringArg(args, "chars", ""); chars != "" {
		if err := s.Write(chars); err != nil && err != io.ErrClosedPipe {
			return nil, err
		}
	}
	select {
	case err := <-s.Done:
		s.Cancel(); r.sessions.Delete(s.ID)
		result := s.Snapshot("exited", intArg(args, "max_output_bytes", 65536))
		if err != nil && !s.TimedOut { result["error"] = err.Error() }
		return result, nil
	default:
		return s.Snapshot("running", intArg(args, "max_output_bytes", 65536)), nil
	}
}

func (r *Runtime) killSession(args map[string]any) (Result, error) {
	s, ok := r.sessions.Get(stringArg(args, "session_id", ""))
	if !ok { return nil, toolError("SESSION_NOT_FOUND", "session not found", "not_found") }
	s.Kill(); r.sessions.Delete(s.ID)
	return s.Snapshot("killed", intArg(args, "max_output_bytes", 65536)), nil
}

func (r *Runtime) commandEnv(extra map[string]any) []string {
	env := map[string]string{}
	for _, key := range []string{"PATH", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value := os.Getenv(key); value != "" { env[key] = value }
	}
	env["HOME"] = r.ws.Root()
	env["TMPDIR"] = filepath.Join(r.ws.Root(), ".tmp")
	_ = os.MkdirAll(env["TMPDIR"], 0o755)
	if r.cfg.DangerouslySkipAllPermissions {
		for key, value := range extra { env[key] = fmt.Sprint(value) }
	}
	out := make([]string, 0, len(env))
	for key, value := range env { out = append(out, key+"="+value) }
	return out
}

