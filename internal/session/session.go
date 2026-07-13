package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/textutil"
)

type PrepareFunc func(*exec.Cmd) (func(), map[string]any)

type CommandFactory func(context.Context) *exec.Cmd

type ExecutionContext struct {
	Runtime      string
	Distribution string
	Workdir      string
}

type Session struct {
	ID         string
	Command    *exec.Cmd
	Cancel     context.CancelFunc
	Stdin      io.WriteCloser
	StartedAt  time.Time
	FinishedAt time.Time
	Done       chan struct{}
	TimedOut   bool
	Terminal   string
	execution  ExecutionContext

	runner commandRunner

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
	ID           string `json:"id"`
	Status       string `json:"status"`
	ElapsedMS    int64  `json:"elapsed_ms"`
	TimedOut     bool   `json:"timed_out"`
	Runtime      string `json:"runtime,omitempty"`
	Distribution string `json:"wsl_distribution,omitempty"`
	Workdir      string `json:"workdir,omitempty"`
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
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out
}

func (s *Store) PruneCompletedBefore(cutoff time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for id, session := range s.sessions {
		if session.CompletedBefore(cutoff) {
			delete(s.sessions, id)
			removed++
		}
	}
	return removed
}

func (s *Session) Summary() Summary {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := "running"
	finishedAt := time.Now()
	if s.completed {
		status = "exited"
		finishedAt = s.FinishedAt
		if s.TimedOut {
			status = "timeout"
		}
	}
	return Summary{
		ID:           s.ID,
		Status:       status,
		ElapsedMS:    finishedAt.Sub(s.StartedAt).Milliseconds(),
		TimedOut:     s.TimedOut,
		Runtime:      s.execution.Runtime,
		Distribution: s.execution.Distribution,
		Workdir:      s.execution.Workdir,
	}
}

func (s *Session) CompletedBefore(cutoff time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completed && !s.FinishedAt.IsZero() && s.FinishedAt.Before(cutoff)
}

func Start(ctx context.Context, command, workdir string, env []string, timeout time.Duration, prepare PrepareFunc) (*Session, map[string]any, error) {
	return StartWithTTY(ctx, command, workdir, env, timeout, false, prepare)
}

func StartWithTTY(ctx context.Context, command, workdir string, env []string, timeout time.Duration, tty bool, prepare PrepareFunc) (*Session, map[string]any, error) {
	return StartCommandWithTTY(ctx, func(cmdCtx context.Context) *exec.Cmd {
		cmd := shellCommand(cmdCtx, command)
		cmd.Dir = workdir
		cmd.Env = env
		return cmd
	}, timeout, tty, prepare)
}

func StartCommandWithTTY(ctx context.Context, build CommandFactory, timeout time.Duration, tty bool, prepare PrepareFunc) (*Session, map[string]any, error) {
	if timeout <= 0 {
		return nil, nil, fmt.Errorf("timeout must be positive")
	}
	if build == nil {
		return nil, nil, fmt.Errorf("command factory is required")
	}
	id, err := newID()
	if err != nil {
		return nil, nil, fmt.Errorf("generate session id: %w", err)
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	cmd := build(cmdCtx)
	if cmd == nil {
		cancel()
		return nil, nil, fmt.Errorf("command factory returned nil command")
	}
	cleanup := func() {}
	status := map[string]any{"enabled": false}
	if prepare != nil {
		cleanup, status = prepare(cmd)
		if cleanup == nil {
			cleanup = func() {}
		}
	}

	s := &Session{
		ID: id, Command: cmd, Cancel: cancel,
		StartedAt: time.Now(), Done: make(chan struct{}), exitCode: -1,
		Terminal: "pipes",
	}
	stdout := sessionOutputWriter{session: s}
	stderr := sessionOutputWriter{session: s, stderr: true}

	var runner commandRunner
	usedInteractive := false
	if tty {
		runner, usedInteractive, err = startInteractiveRunner(cmdCtx, cmd, stdout, stderr)
	}
	if err == nil && !usedInteractive {
		runner, err = startStandardRunner(cmd, stdout, stderr)
	}
	if err != nil {
		cancel()
		cleanup()
		return nil, status, err
	}
	if usedInteractive {
		s.Terminal = "conpty"
	}
	s.runner = runner
	s.Stdin = runner.Stdin()
	cleanup()

	// CommandContext only guarantees termination of the direct child. Every runner
	// therefore owns a platform process tree and receives timeout/cancel events.
	go func() {
		select {
		case <-cmdCtx.Done():
			_ = runner.Kill()
		case <-s.Done:
		}
	}()

	go func() {
		exitCode, waitErr := runner.Wait()
		s.mu.Lock()
		s.waitErr = waitErr
		s.completed = true
		s.FinishedAt = time.Now()
		s.exitCode = exitCode
		if cmdCtx.Err() == context.DeadlineExceeded {
			s.TimedOut = true
		}
		s.mu.Unlock()
		close(s.Done)
	}()
	return s, status, nil
}

func (s *Session) SetExecutionContext(execution ExecutionContext) {
	s.mu.Lock()
	s.execution = execution
	s.mu.Unlock()
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

func (s *Session) Kill() bool {
	s.mu.Lock()
	if s.completed {
		s.mu.Unlock()
		return false
	}
	runner := s.runner
	s.mu.Unlock()
	if runner == nil {
		return false
	}
	_ = runner.Kill()
	s.Cancel()
	return true
}

func (s *Session) Snapshot(status string, maxBytes int) map[string]any {
	return s.snapshot(status, maxBytes, true)
}

// Peek returns the current unread output without advancing the observation cursors.
// Mutation tools use it so a following status call still receives the output.
func (s *Session) Peek(status string, maxBytes int) map[string]any {
	return s.snapshot(status, maxBytes, false)
}

func (s *Session) snapshot(status string, maxBytes int, advance bool) map[string]any {
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
	if advance {
		s.stdoutCursor = len(stdoutFull)
		s.stderrCursor = len(stderrFull)
	}
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
		"terminal":             s.Terminal,
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
	if s.execution.Runtime != "" {
		result["runtime"] = s.execution.Runtime
	}
	if s.execution.Distribution != "" {
		result["wsl_distribution"] = s.execution.Distribution
	}
	if s.execution.Workdir != "" {
		result["workdir"] = s.execution.Workdir
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
