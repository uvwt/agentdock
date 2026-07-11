package skillruntime

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// Event is a local runtime envelope for future runtime observers.
type Event struct {
	Type      string         `json:"type"`
	RunID     string         `json:"run_id,omitempty"`
	Skill     string         `json:"skill"`
	Version   string         `json:"version,omitempty"`
	Operation string         `json:"operation,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload,omitempty"`
}

type EventSink interface {
	Emit(context.Context, Event) error
}

func (r *Runtime) emit(ctx context.Context, event Event) {
	if r.Events == nil {
		return
	}
	if err := r.Events.Emit(ctx, event); err != nil {
		slog.WarnContext(ctx, "skill runtime event delivery failed",
			"event_type", event.Type,
			"skill", event.Skill,
			"operation", event.Operation,
			"error", err,
		)
	}
}

func errorStage(err error) string {
	var runtimeErr *Error
	if errors.As(err, &runtimeErr) {
		return runtimeErr.Stage
	}
	return "execute"
}
