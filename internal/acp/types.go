package acp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	ProtocolVersion            = 1
	maxEventCount              = 512
	maxEventBytes              = 4 << 20
	maxEventUpdateBytes        = 512 << 10
	maxEventPreviewBytes       = 64 << 10
	maxTrackedRemoteErrors     = 16
	maxTrackedRemoteErrorBytes = 16 << 10
)

type AgentSpec struct {
	Name        string
	Command     string
	Args        []string
	Environment map[string]string
}

type Options struct {
	Home               string
	DefaultCWD         string
	Agent              AgentSpec
	MaxConcurrentRuns  int
	InteractionTimeout time.Duration
}

type Error struct {
	Code      string
	Message   string
	Retryable bool
	Details   map[string]any
	Cause     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

func newError(code, message string, retryable bool, details map[string]any, cause error) *Error {
	return &Error{Code: code, Message: message, Retryable: retryable, Details: details, Cause: cause}
}

func errorCode(err error) string {
	var acpErr *Error
	if errors.As(err, &acpErr) {
		return acpErr.Code
	}
	return "ACP_INTERNAL"
}

type AgentInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

type InitializeResult struct {
	ProtocolVersion   int            `json:"protocolVersion"`
	AgentCapabilities map[string]any `json:"agentCapabilities"`
	AgentInfo         AgentInfo      `json:"agentInfo"`
	AuthMethods       []any          `json:"authMethods,omitempty"`
	Meta              map[string]any `json:"_meta,omitempty"`
}

type SessionStatus string

const (
	SessionReady       SessionStatus = "ready"
	SessionRunning     SessionStatus = "running"
	SessionInterrupted SessionStatus = "interrupted"
	SessionClosed      SessionStatus = "closed"
	SessionError       SessionStatus = "error"
	sessionDeleted     SessionStatus = "deleted"
)

type SessionRecord struct {
	SchemaVersion         int           `json:"schema_version"`
	ID                    string        `json:"id"`
	Agent                 string        `json:"agent"`
	RemoteSessionID       string        `json:"remote_session_id"`
	CWD                   string        `json:"cwd"`
	AdditionalDirectories []string      `json:"additional_directories,omitempty"`
	ModeID                string        `json:"mode_id,omitempty"`
	Status                SessionStatus `json:"status"`
	LastStopReason        string        `json:"last_stop_reason,omitempty"`
	CreatedAt             time.Time     `json:"created_at"`
	UpdatedAt             time.Time     `json:"updated_at"`
	ClosedAt              *time.Time    `json:"closed_at,omitempty"`
}

type RunStatus string

const (
	RunRunning     RunStatus = "running"
	RunCompleted   RunStatus = "completed"
	RunCancelled   RunStatus = "cancelled"
	RunInterrupted RunStatus = "interrupted"
	RunFailed      RunStatus = "failed"
)

type Event struct {
	Seq                 uint64          `json:"seq"`
	Source              string          `json:"source"`
	Type                string          `json:"type"`
	SessionID           string          `json:"session_id"`
	RunID               string          `json:"run_id,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	Update              json.RawMessage `json:"update,omitempty"`
	UpdateTruncated     bool            `json:"update_truncated,omitempty"`
	OriginalUpdateBytes int             `json:"original_update_bytes,omitempty"`
	StopReason          string          `json:"stop_reason,omitempty"`
	ErrorCode           string          `json:"error_code,omitempty"`
	Message             string          `json:"message,omitempty"`
}

type eventPage struct {
	Events       []Event
	NextSeq      uint64
	FirstSeq     uint64
	LatestSeq    uint64
	DroppedCount uint64
	HasMore      bool
	Truncated    bool
}

type Run struct {
	ID                    string
	SessionID             string
	Status                RunStatus
	StartedAt             time.Time
	EndedAt               *time.Time
	StopReason            string
	Err                   error
	CancelRequested       bool
	cancel                func()
	eventsMu              sync.Mutex
	events                []Event
	eventBytes            int
	nextSeq               uint64
	notify                chan struct{}
	remoteErrorCandidates map[string]string
	lastAssistantChunk    string
	finalized             chan struct{}
	finalizeOnce          sync.Once
}

func newRun(id, sessionID string) *Run {
	return &Run{
		ID: id, SessionID: sessionID, Status: RunRunning, StartedAt: time.Now().UTC(),
		nextSeq: 1, notify: make(chan struct{}, 1), finalized: make(chan struct{}),
		remoteErrorCandidates: make(map[string]string),
	}
}

func (r *Run) appendSessionUpdate(event Event) {
	if r == nil {
		return
	}
	r.eventsMu.Lock()
	r.noteCodexUpdateLocked(event.Type, event.Update)
	r.appendEventLocked(event)
	r.eventsMu.Unlock()
	r.signalEvent()
}

func (r *Run) noteCodexUpdateLocked(eventType string, update json.RawMessage) {
	switch eventType {
	case "session_info_update":
		var payload struct {
			Meta struct {
				Codex struct {
					Error *struct {
						Message           string `json:"message"`
						AdditionalDetails string `json:"additionalDetails"`
						WillRetry         bool   `json:"willRetry"`
					} `json:"error"`
				} `json:"codex"`
			} `json:"_meta"`
		}
		if json.Unmarshal(update, &payload) != nil || payload.Meta.Codex.Error == nil || !payload.Meta.Codex.Error.WillRetry {
			return
		}
		for _, candidate := range []string{payload.Meta.Codex.Error.Message, payload.Meta.Codex.Error.AdditionalDetails} {
			normalized := normalizeRemoteErrorText(candidate)
			if normalized == "" || len(normalized) > maxTrackedRemoteErrorBytes {
				continue
			}
			if _, exists := r.remoteErrorCandidates[normalized]; !exists && len(r.remoteErrorCandidates) >= maxTrackedRemoteErrors {
				continue
			}
			r.remoteErrorCandidates[normalized] = strings.TrimSpace(candidate)
		}
	case "agent_message_chunk":
		if text := normalizeRemoteErrorText(assistantChunkText(update)); text != "" && len(text) <= maxTrackedRemoteErrorBytes {
			r.lastAssistantChunk = text
		} else {
			r.lastAssistantChunk = ""
		}
	}
}

func (r *Run) codexFalseSuccess() (string, bool) {
	if r == nil {
		return "", false
	}
	r.eventsMu.Lock()
	defer r.eventsMu.Unlock()
	message, ok := r.remoteErrorCandidates[r.lastAssistantChunk]
	return message, ok && r.lastAssistantChunk != ""
}

func normalizeRemoteErrorText(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
}

func assistantChunkText(update json.RawMessage) string {
	var payload struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(update, &payload) != nil || len(payload.Content) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(payload.Content, &text) == nil {
		return text
	}
	var content struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(payload.Content, &content) == nil {
		return content.Text
	}
	return ""
}

func (r *Run) appendEvent(event Event) {
	if r == nil {
		return
	}
	r.eventsMu.Lock()
	r.appendEventLocked(event)
	r.eventsMu.Unlock()
	r.signalEvent()
}

func (r *Run) appendEventLocked(event Event) {
	event.Seq = r.nextSeq
	r.nextSeq++
	if event.Source == "" {
		event.Source = "agentdock"
	}
	event.SessionID = r.SessionID
	event.RunID = r.ID
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	event.Update, event.UpdateTruncated, event.OriginalUpdateBytes = boundedEventUpdate(event.Update)
	size := eventSize(event)
	r.events = append(r.events, event)
	r.eventBytes += size
	for len(r.events) > 1 && (len(r.events) > maxEventCount || r.eventBytes > maxEventBytes) {
		removed := r.events[0]
		r.eventBytes -= eventSize(removed)
		r.events = r.events[1:]
	}
}

func (r *Run) signalEvent() {
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

func (r *Run) eventsAfter(after uint64, limit int) (eventPage, error) {
	r.eventsMu.Lock()
	defer r.eventsMu.Unlock()
	return r.eventsAfterLocked(after, limit)
}

func (r *Run) eventsAfterLocked(after uint64, limit int) (eventPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	latestSeq := r.nextSeq - 1
	if after > latestSeq {
		return eventPage{}, newError(
			"ACP_CURSOR_AHEAD",
			"after_seq is newer than the latest ACP event",
			false,
			map[string]any{"after_seq": after, "latest_seq": latestSeq},
			nil,
		)
	}
	out := make([]Event, 0, limit)
	cursor := after
	for _, event := range r.events {
		if event.Seq <= after {
			continue
		}
		out = append(out, event)
		cursor = event.Seq
		if len(out) == limit {
			break
		}
	}
	firstSeq := r.nextSeq
	if len(r.events) > 0 {
		firstSeq = r.events[0].Seq
	}
	truncated := after < firstSeq-1
	droppedCount := uint64(0)
	if firstSeq > 1 {
		droppedCount = firstSeq - 1
	}
	return eventPage{
		Events: out, NextSeq: cursor, FirstSeq: firstSeq, LatestSeq: latestSeq,
		DroppedCount: droppedCount, HasMore: cursor < latestSeq, Truncated: truncated,
	}, nil
}

func eventSize(event Event) int {
	return len(event.Update) + len(event.Message) + len(event.StopReason) + 160
}

func boundedEventUpdate(update json.RawMessage) (json.RawMessage, bool, int) {
	originalBytes := len(update)
	if originalBytes == 0 {
		return nil, false, 0
	}
	if originalBytes <= maxEventUpdateBytes {
		return append(json.RawMessage(nil), update...), false, 0
	}
	var typed struct {
		SessionUpdate string `json:"sessionUpdate"`
	}
	_ = json.Unmarshal(update, &typed)
	preview := truncateUTF8(string(update), maxEventPreviewBytes)
	summary := map[string]any{
		"_agentdock": map[string]any{
			"truncated":      true,
			"original_bytes": originalBytes,
			"retained_bytes": len(preview),
		},
		"preview": preview,
	}
	if typed.SessionUpdate != "" {
		summary["sessionUpdate"] = typed.SessionUpdate
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		encoded = []byte(`{"_agentdock":{"truncated":true}}`)
	}
	return encoded, true, originalBytes
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = string([]byte(value)[:limit])
	for !utf8.ValidString(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	return strings.ToValidUTF8(value, "�")
}

type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

type InteractionStatus string

const (
	InteractionPending   InteractionStatus = "pending"
	InteractionResponded InteractionStatus = "responded"
	InteractionCancelled InteractionStatus = "cancelled"
	InteractionExpired   InteractionStatus = "expired"
)

type Interaction struct {
	ID        string             `json:"id"`
	SessionID string             `json:"session_id"`
	Kind      string             `json:"kind"`
	Status    InteractionStatus  `json:"status"`
	ToolCall  map[string]any     `json:"tool_call,omitempty"`
	Options   []PermissionOption `json:"options,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
	ExpiresAt time.Time          `json:"expires_at"`
	respond   chan interactionResponse
}

type interactionResponse struct {
	optionID  string
	cancelled bool
}

func (i *Interaction) public() Interaction {
	if i == nil {
		return Interaction{}
	}
	return Interaction{
		ID: i.ID, SessionID: i.SessionID, Kind: i.Kind, Status: i.Status,
		ToolCall: cloneMap(i.ToolCall), Options: append([]PermissionOption(nil), i.Options...),
		CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt,
	}
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}
