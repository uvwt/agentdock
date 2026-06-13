package commandqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	contracts "github.com/uvwt/agentdock/generated/nexuscontracts"
)

const (
	defaultCommandTimeout  = 5 * time.Minute
	defaultMaxOutputBytes  = 4 << 20
	artifactCommandTimeout = 30 * time.Minute
)

type ProgressReporter interface {
	Report(context.Context, contracts.CommandProgress) error
}

type ProgressFunc func(context.Context, contracts.CommandProgress) error

func (f ProgressFunc) Report(ctx context.Context, progress contracts.CommandProgress) error {
	if f == nil {
		return nil
	}
	return f(ctx, progress)
}

type HandlerResult struct{ Output any }

type CommandHandler interface {
	Type() string
	Execute(context.Context, json.RawMessage, ProgressReporter) (HandlerResult, error)
}

type FuncHandler struct {
	CommandType string
	Run         func(context.Context, json.RawMessage, ProgressReporter) (HandlerResult, error)
}

func (h FuncHandler) Type() string { return h.CommandType }
func (h FuncHandler) Execute(ctx context.Context, payload json.RawMessage, progress ProgressReporter) (HandlerResult, error) {
	if h.Run == nil {
		return HandlerResult{}, errors.New("handler function is nil")
	}
	return h.Run(ctx, payload, progress)
}

type HandlerError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *HandlerError) Error() string { return e.Message }

type Execution struct {
	Result    contracts.CommandResult
	Duplicate bool
}

type Executor struct {
	store          *Store
	allowed        map[string]struct{}
	handlers       map[string]CommandHandler
	defaultTimeout time.Duration
	maxOutputBytes int
	now            func() time.Time
	mu             sync.RWMutex
}

func NewExecutor(store *Store) *Executor {
	allowed := map[string]struct{}{}
	for _, commandType := range DefaultAllowedCommandTypes() {
		allowed[commandType] = struct{}{}
	}
	return &Executor{store: store, allowed: allowed, handlers: map[string]CommandHandler{}, defaultTimeout: defaultCommandTimeout, maxOutputBytes: defaultMaxOutputBytes, now: time.Now}
}

func DefaultAllowedCommandTypes() []string {
	return []string{"health.check", "skill.install", "skill.run", "skill.rollback", "memory.sync", "service.inspect", "service.restart", "diagnostics.collect", "agentdock.reload", "env.manage", "artifact.pull", "artifact.fetch"}
}

func (e *Executor) Register(handler CommandHandler) error {
	if handler == nil || handler.Type() == "" {
		return errors.New("command handler and type are required")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, allowed := e.allowed[handler.Type()]; !allowed {
		return fmt.Errorf("command type %q is not allowed", handler.Type())
	}
	if _, exists := e.handlers[handler.Type()]; exists {
		return fmt.Errorf("command handler %q already registered", handler.Type())
	}
	e.handlers[handler.Type()] = handler
	return nil
}

func (e *Executor) Execute(ctx context.Context, lease contracts.CommandLease, progress ProgressReporter) (Execution, error) {
	now := e.now().UTC()
	cmd := lease.Command
	if _, allowed := e.allowed[cmd.Type]; !allowed {
		return Execution{Result: failedResult(cmd.Id, lease.LeaseId, now, now, "COMMAND_NOT_ALLOWED", "command type is not allowed")}, nil
	}
	commandExpiry, err := time.Parse(time.RFC3339, cmd.ExpiresAt)
	if err != nil {
		return Execution{}, fmt.Errorf("invalid command expires_at: %w", err)
	}
	if !commandExpiry.After(now) {
		result := failedResult(cmd.Id, lease.LeaseId, now, now, "COMMAND_EXPIRED", "command expired before execution")
		result.Status = StatusExpired
		return Execution{Result: result}, nil
	}
	leaseExpiry, err := time.Parse(time.RFC3339, lease.ExpiresAt)
	if err != nil {
		return Execution{}, fmt.Errorf("invalid lease expires_at: %w", err)
	}
	if !leaseExpiry.After(now) {
		return Execution{Result: failedResult(cmd.Id, lease.LeaseId, now, now, "LEASE_EXPIRED", "command lease expired before execution")}, nil
	}
	if e.store == nil {
		return Execution{}, errors.New("command store is required")
	}
	record, execute, err := e.store.Begin(lease)
	if err != nil {
		return Execution{}, err
	}
	if !execute {
		if record.Result != nil {
			return Execution{Result: *record.Result, Duplicate: true}, nil
		}
		return Execution{Duplicate: true}, nil
	}

	e.mu.RLock()
	handler := e.handlers[cmd.Type]
	e.mu.RUnlock()
	if handler == nil {
		result := failedResult(cmd.Id, lease.LeaseId, record.StartedAt, e.now().UTC(), "HANDLER_MISSING", "no local handler is registered")
		if err := e.store.Complete(cmd.Id, result); err != nil {
			return Execution{}, err
		}
		return Execution{Result: result}, nil
	}

	timeout := e.defaultTimeout
	if cmd.Type == "artifact.pull" || cmd.Type == "artifact.fetch" {
		// Artifact transfers can legitimately outlive the initial short lease.
		// The Nexus agent renews that lease while this handler is running.
		timeout = artifactCommandTimeout
	} else if untilLeaseExpiry := time.Until(leaseExpiry); untilLeaseExpiry < timeout {
		timeout = untilLeaseExpiry
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type response struct {
		result HandlerResult
		err    error
	}
	responseCh := make(chan response, 1)
	go func() {
		result, err := handler.Execute(runCtx, append(json.RawMessage(nil), cmd.Payload...), progress)
		responseCh <- response{result: result, err: err}
	}()
	var handlerResult HandlerResult
	var handlerErr error
	select {
	case <-runCtx.Done():
		handlerErr = runCtx.Err()
	case response := <-responseCh:
		handlerResult, handlerErr = response.result, response.err
	}

	completedAt := e.now().UTC()
	var result contracts.CommandResult
	if handlerErr != nil {
		code, message := normalizeHandlerError(handlerErr)
		result = failedResult(cmd.Id, lease.LeaseId, record.StartedAt, completedAt, code, message)
	} else {
		output, err := json.Marshal(handlerResult.Output)
		if err != nil {
			result = failedResult(cmd.Id, lease.LeaseId, record.StartedAt, completedAt, "OUTPUT_ENCODE_FAILED", err.Error())
		} else if len(output) > e.maxOutputBytes {
			result = failedResult(cmd.Id, lease.LeaseId, record.StartedAt, completedAt, "OUTPUT_TOO_LARGE", "command output exceeds local limit")
		} else {
			result = contracts.CommandResult{CommandId: cmd.Id, LeaseId: lease.LeaseId, Status: StatusSucceeded, StartedAt: record.StartedAt.UTC().Format(time.RFC3339Nano), CompletedAt: completedAt.Format(time.RFC3339Nano), Output: output}
		}
	}
	if err := e.store.Complete(cmd.Id, result); err != nil {
		return Execution{}, err
	}
	return Execution{Result: result}, nil
}

func failedResult(commandID, leaseID string, startedAt, completedAt time.Time, code, message string) contracts.CommandResult {
	return contracts.CommandResult{CommandId: commandID, LeaseId: leaseID, Status: StatusFailed, StartedAt: startedAt.UTC().Format(time.RFC3339Nano), CompletedAt: completedAt.UTC().Format(time.RFC3339Nano), Output: json.RawMessage(`null`), Error: &contracts.ErrorResponse{Code: code, Message: message, RequestId: "local"}}
}

func normalizeHandlerError(err error) (string, string) {
	var handlerErr *HandlerError
	if errors.As(err, &handlerErr) {
		return handlerErr.Code, handlerErr.Message
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "LOCAL_TIMEOUT", "command exceeded local timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "LOCAL_CANCELLED", "command was cancelled locally"
	}
	return "HANDLER_FAILED", err.Error()
}
