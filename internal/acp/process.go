package acp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	processcontrol "github.com/uvwt/agentdock/internal/process"
)

type agentProcess struct {
	spec       AgentSpec
	command    *exec.Cmd
	controller *processcontrol.Controller
	connection *Connection
	stderr     *tailBuffer
	initialize InitializeResult

	closeOnce sync.Once
	closeErr  error
}

func startAgentProcess(ctx context.Context, spec AgentSpec, defaultCWD string, requestHandler RequestHandler, notificationHandler NotificationHandler) (*agentProcess, error) {
	environment := envstore.Merge(envstore.MinimalSystemEnv(), spec.Environment)
	// The adapter must outlive ctx, which only bounds startup and initialize.
	// agentProcess.Close owns termination through processcontrol.Controller.
	command := exec.Command(spec.Command, spec.Args...) //nolint:noctx // adapter lifetime exceeds the startup context
	command.Env = envstore.Format(environment)
	command.Dir = defaultCWD
	stderr := newTailBuffer(64 << 10)
	command.Stderr = stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, newError("ACP_START_FAILED", "open ACP stdout", false, map[string]any{"agent": spec.Name}, err)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		_ = stdout.Close()
		return nil, newError("ACP_START_FAILED", "open ACP stdin", false, map[string]any{"agent": spec.Name}, err)
	}
	configureAgentCommand(command)
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, newError("ACP_START_FAILED", "start ACP agent", false, map[string]any{"agent": spec.Name, "command": spec.Command}, err)
	}
	controller, err := processcontrol.Attach(command)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, newError("ACP_START_FAILED", "attach ACP process controller", false, map[string]any{"agent": spec.Name}, err)
	}
	process := &agentProcess{
		spec: spec, command: command, controller: controller, stderr: stderr,
	}
	process.connection = NewConnection(stdout, stdin, requestHandler, notificationHandler)

	initializeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var initialized InitializeResult
	err = process.connection.Request(initializeCtx, "initialize", map[string]any{
		"protocolVersion":    ProtocolVersion,
		"clientCapabilities": map[string]any{},
		"clientInfo": map[string]any{
			"name": config.ServerName, "title": "AgentDock", "version": buildinfo.Version,
		},
	}, &initialized)
	if err != nil {
		_ = process.Close()
		return nil, process.wrapError("initialize ACP agent", err)
	}
	if initialized.ProtocolVersion != ProtocolVersion {
		_ = process.Close()
		return nil, newError(
			"ACP_PROTOCOL_UNSUPPORTED",
			"ACP agent selected an unsupported protocol version",
			false,
			map[string]any{"agent": spec.Name, "protocol_version": initialized.ProtocolVersion, "supported": ProtocolVersion},
			nil,
		)
	}
	// ACP 对省略的 capabilities/authMethods 分别定义为空能力集与空认证列表。
	// 在协议边界归一化，避免内部状态和后续 MCP structuredContent 携带 nil。
	if initialized.AgentCapabilities == nil {
		initialized.AgentCapabilities = map[string]any{}
	}
	if initialized.AuthMethods == nil {
		initialized.AuthMethods = []any{}
	}
	process.initialize = initialized
	return process, nil
}

func (p *agentProcess) Close() error {
	if p == nil {
		return nil
	}
	p.closeOnce.Do(func() {
		if p.connection != nil {
			// EOF/closed-pipe is the normal terminal state when AgentDock closes the
			// adapter's stdio. Request paths already preserve unexpected connection
			// failures with stderr diagnostics; shutdown must remain idempotent.
			_ = p.connection.Close()
		}
		if p.controller != nil {
			_ = p.controller.Terminate()
			p.closeErr = errors.Join(p.closeErr, p.controller.Close())
			p.controller = nil
		}
		if p.command != nil {
			if err := p.command.Wait(); err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) && !errors.Is(err, os.ErrProcessDone) {
					p.closeErr = errors.Join(p.closeErr, err)
				}
			}
			p.command = nil
		}
	})
	return p.closeErr
}

func (p *agentProcess) wrapError(operation string, err error) error {
	if err == nil {
		return nil
	}
	details := map[string]any{"agent": p.spec.Name}
	if p.stderr != nil {
		if stderr := p.stderr.String(); stderr != "" {
			details["stderr"] = redactEnvironmentValues(stderr, p.spec.Environment)
		}
	}
	var acpErr *Error
	if errors.As(err, &acpErr) {
		for key, value := range acpErr.Details {
			if text, ok := value.(string); ok {
				details[key] = redactAndLimit(text, p.spec.Environment, 65536)
				continue
			}
			details[key] = value
		}
		message := redactAndLimit(acpErr.Message, p.spec.Environment, 8192)
		return newError(acpErr.Code, message, acpErr.Retryable, details, acpErr.Cause)
	}
	return newError("ACP_CONNECTION_FAILED", operation, true, details, err)
}

func redactEnvironmentValues(text string, environment map[string]string) string {
	redacted := text
	for _, value := range environment {
		if len(value) < 4 {
			continue
		}
		redacted = strings.ReplaceAll(redacted, value, "[REDACTED]")
	}
	return redacted
}

func redactAndLimit(text string, environment map[string]string, limit int) string {
	redacted := redactEnvironmentValues(text, environment)
	if limit <= 0 || len(redacted) <= limit {
		return redacted
	}
	return redacted[:limit] + "...[truncated]"
}

type tailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func newTailBuffer(limit int) *tailBuffer {
	return &tailBuffer{limit: limit, data: make([]byte, 0, limit)}
}

func (b *tailBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(data)
	if b.limit <= 0 {
		return original, nil
	}
	if len(data) >= b.limit {
		b.data = append(b.data[:0], data[len(data)-b.limit:]...)
		return original, nil
	}
	if overflow := len(b.data) + len(data) - b.limit; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
	}
	b.data = append(b.data, data...)
	return original, nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(bytes.TrimSpace(append([]byte(nil), b.data...)))
}
