package goal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/atomicfile"
)

// EventLog appends durable audit events per goal.
type EventLog struct {
	root string
	mu   sync.Mutex
}

func newEventLog(root string) (*EventLog, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create goal event root: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure goal event root: %w", err)
	}
	return &EventLog{root: root}, nil
}

type logRecord struct {
	EventID   string         `json:"event_id"`
	GoalID    string         `json:"goal_id"`
	Type      string         `json:"type"`
	Summary   string         `json:"summary"`
	Timestamp time.Time      `json:"timestamp"`
	Payload   map[string]any `json:"payload,omitempty"`
}

func (l *EventLog) Append(goalID, eventType, summary string, payload map[string]any) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	id, err := newPrefixedID("evt")
	if err != nil {
		return err
	}
	rec := logRecord{
		EventID:   id,
		GoalID:    goalID,
		Type:      eventType,
		Summary:   summary,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	path := filepath.Join(l.root, goalID+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open goal event log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append goal event log: %w", err)
	}
	return nil
}

// ensurePrivateFile rewrites a file with private perms when needed (used by tests / recovery).
func ensurePrivateFile(path string, data []byte) error {
	return atomicfile.Write(path, data, 0o600)
}
