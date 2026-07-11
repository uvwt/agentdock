package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/uvwt/agentdock/internal/textutil"
)

type PrepareFunc func(*exec.Cmd) (func(), map[string]any)

type Session struct {
	ID        string
	Command   *exec.Cmd
	Cancel    context.CancelFunc
	Stdin     io.WriteCloser
	StartedAt time.Time
	Done      chan struct{}
	TimedOut  bool

	mu                 sync.Mutex
	completed          bool
	exitCode           int
	waitErr            error
	stdout             bytes.Buffer
	stderr             bytes.Buffer
	stdoutTotalBytes   int
	stderrTotalBytes   int
	stdoutDroppedBytes int
	stderrDroppedBytes int
	stdoutCursor       int
	stderrCursor       int
}

type Store struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

type Summary struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	ElapsedMS int64  `json:"elapsed_ms"`
	TimedOut  bool   `json:"timed_out"`
}

func NewStore() *Store {
	return &Store{sessions: map[string]*Session{}}
}

func (s *Store) Add(session *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
}

func (s *Store) Get(id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	return session, ok
}

func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *Store) List() []*Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		out = append(out, session)
	}
	return out
}

func (s *Session) Summary(status string) Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Summary{ID: s.ID, Status: status, ElapsedMS: time.Since(s.StartedAt).Milliseconds(), TimedOut: s.TimedOut}
}

func Start(ctx context.Context, command, workdir string, env []string, timeout time.Duration, prepare PrepareFunc) (*Session, map[string]any, error) {
	if timeout <= 0 {
		return nil, nil, fmt.Errorf("timeout must be positive")
	}
	id, err := newID()
	if err != nil {
		return nil, nil, fmt.Errorf("generate session id: %w", err)
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	cmd := ShellCommand(cmdCtx, command)
	cmd.Dir = workdir
	cmd.Env = env
	setProcessGroup(cmd)
	cleanup := func() {}
	status := map[string]any{"enabled": false}
	if prepare != nil {
		cleanup, status = prepare(cmd)
		if cleanup == nil {
			cleanup = func() {}
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		cleanup()
		return nil, status, err
	}
	s := &Session{
		ID: id, Command: cmd, Cancel: cancel, Stdin: stdin,
		StartedAt: time.Now(), Done: make(chan struct{}), exitCode: -1,
	}
	cmd.Stdout = sessionOutputWriter{session: s}
	cmd.Stderr = sessionOutputWriter{session: s, stderr: true}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		cleanup()
		return nil, status, err
	}
	cleanup()

	go func() {
		// exec.Cmd.Wait 会等待其内部 stdout/stderr 复制协程完成，因此完成快照
		// 不会与快速退出命令争抢管道关闭时机，也不会遗漏尾部输出。
		waitErr := cmd.Wait()
		s.mu.Lock()
		s.waitErr = waitErr
		s.completed = true
		if cmd.ProcessState != nil {
			s.exitCode = cmd.ProcessState.ExitCode()
		}
		if cmdCtx.Err() == context.DeadlineExceeded {
			s.TimedOut = true
		}
		s.mu.Unlock()
		close(s.Done)
	}()
	return s, status, nil
}

func (s *Session) Write(text string) error {
	_, err := io.WriteString(s.Stdin, text)
	return err
}

func (s *Session) CloseStdin() error { return s.Stdin.Close() }

func (s *Session) WaitError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waitErr
}

func (s *Session) Kill() {
	if s.Command.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = s.Command.Process.Kill()
	} else {
		_ = syscall.Kill(-s.Command.Process.Pid, syscall.SIGTERM)
	}
	s.Cancel()
}

func (s *Session) Snapshot(status string, maxBytes int) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	stdoutFull := s.stdout.String()
	stderrFull := s.stderr.String()
	stdoutSegment := stdoutFull
	stderrSegment := stderrFull
	if s.stdoutCursor > 0 && s.stdoutCursor <= len(stdoutFull) {
		stdoutSegment = stdoutFull[s.stdoutCursor:]
	}
	if s.stderrCursor > 0 && s.stderrCursor <= len(stderrFull) {
		stderrSegment = stderrFull[s.stderrCursor:]
	}
	s.stdoutCursor = len(stdoutFull)
	s.stderrCursor = len(stderrFull)
	stdout := trim(stdoutSegment, maxBytes)
	stderr := trim(stderrSegment, maxBytes)
	result := map[string]any{
		"ok":                   true,
		"session_id":           s.ID,
		"status":               status,
		"stdout":               stdout,
		"stderr":               stderr,
		"elapsed_ms":           time.Since(s.StartedAt).Milliseconds(),
		"timed_out":            s.TimedOut,
		"stdout_output_bytes":  len([]byte(stdout)),
		"stderr_output_bytes":  len([]byte(stderr)),
		"stdout_total_bytes":   s.stdoutTotalBytes,
		"stderr_total_bytes":   s.stderrTotalBytes,
		"stdout_dropped_bytes": s.stdoutDroppedBytes,
		"stderr_dropped_bytes": s.stderrDroppedBytes,
		"stdout_omitted_bytes": omittedBytes(stdoutSegment, maxBytes),
		"stderr_omitted_bytes": omittedBytes(stderrSegment, maxBytes),
		"stdout_output_lines":  countLines(stdout),
		"stderr_output_lines":  countLines(stderr),
		"stdout_truncated":     maxBytes > 0 && len([]byte(stdoutSegment)) > maxBytes,
		"stderr_truncated":     maxBytes > 0 && len([]byte(stderrSegment)) > maxBytes,
	}
	if s.completed {
		result["exit_code"] = s.exitCode
	}
	return result
}

type sessionOutputWriter struct {
	session *Session
	stderr  bool
}

func (w sessionOutputWriter) Write(data []byte) (int, error) {
	s := w.session
	s.mu.Lock()
	defer s.mu.Unlock()

	dst := &s.stdout
	if w.stderr {
		dst = &s.stderr
	}
	n, err := dst.Write(data)
	if w.stderr {
		s.stderrTotalBytes += n
		dropped := trimBuffer(dst, 4*1024*1024)
		s.stderrDroppedBytes += dropped
		s.stderrCursor = adjustCursorAfterDrop(s.stderrCursor, dropped)
	} else {
		s.stdoutTotalBytes += n
		dropped := trimBuffer(dst, 4*1024*1024)
		s.stdoutDroppedBytes += dropped
		s.stdoutCursor = adjustCursorAfterDrop(s.stdoutCursor, dropped)
	}
	return n, err
}

func ShellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return exec.CommandContext(ctx, shell, "-c", command)
}

func setProcessGroup(cmd *exec.Cmd) {
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
}

func trim(value string, maxBytes int) string {
	return textutil.SafeTruncateString(value, maxBytes).Text
}

func omittedBytes(value string, maxBytes int) int {
	if maxBytes <= 0 || len([]byte(value)) <= maxBytes {
		return 0
	}
	return len([]byte(value)) - len([]byte(trim(value, maxBytes)))
}

func countLines(value string) int {
	if value == "" {
		return 0
	}
	count := strings.Count(value, "\n")
	if !strings.HasSuffix(value, "\n") {
		count++
	}
	return count
}

func trimBuffer(buf *bytes.Buffer, limit int) int {
	if limit <= 0 || buf.Len() <= limit {
		return 0
	}
	data := buf.Bytes()
	dropped := len(data) - limit
	kept := append([]byte(nil), data[dropped:]...)
	buf.Reset()
	_, _ = buf.Write(kept)
	return dropped
}

func adjustCursorAfterDrop(cursor, dropped int) int {
	if dropped <= 0 {
		return cursor
	}
	if cursor <= dropped {
		return 0
	}
	return cursor - dropped
}

func newID() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "session-" + hex.EncodeToString(raw), nil
}
