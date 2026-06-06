package skillruntime

import (
	"context"
	"errors"
	"time"
)

// Event is a local runtime envelope. T4 is responsible for mapping it to the
// generated Nexus contracts and delivering it through the offline outbox.
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
	if r.Events != nil {
		_ = r.Events.Emit(ctx, event)
	}
}

func errorStage(err error) string {
	var runtimeErr *Error
	if errors.As(err, &runtimeErr) {
		return runtimeErr.Stage
	}
	return "execute"
}
