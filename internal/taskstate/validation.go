package taskstate

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/textutil"
)

func normalizeTaskSteps(values []TaskStepInput, now time.Time) ([]TaskStep, error) {
	if len(values) > 12 {
		return nil, errors.New("task steps cannot exceed 12")
	}
	seen := make(map[string]struct{}, len(values))
	steps := make([]TaskStep, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value.ID)
		title := strings.TrimSpace(value.Title)
		if id == "" || title == "" {
			return nil, errors.New("each task step requires id and title")
		}
		if err := validateTextLimit("task step title", title, maxTaskStepTitleBytes); err != nil {
			return nil, err
		}
		if !validStepID(id) {
			return nil, fmt.Errorf("invalid task step id %q", id)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("duplicate task step id %q", id)
		}
		phase := value.Phase
		if phase == "" {
			phase = PhaseExecute
		}
		if phase != PhaseCheck && phase != PhaseExecute && phase != PhaseVerify && phase != PhaseCloseout {
			return nil, fmt.Errorf("invalid task step phase %q", phase)
		}
		seen[id] = struct{}{}
		steps = append(steps, TaskStep{ID: id, Title: title, Phase: phase, Status: StepPending, UpdatedAt: now})
	}
	return steps, nil
}

func normalizeTemplateReferences(values []TemplateReference) ([]TemplateReference, error) {
	if len(values) > 3 {
		return nil, errors.New("source templates cannot exceed 3")
	}
	seen := make(map[string]struct{}, len(values))
	refs := make([]TemplateReference, 0, len(values))
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.Version = strings.TrimSpace(value.Version)
		value.Hash = strings.TrimSpace(value.Hash)
		if value.ID == "" || value.Version == "" {
			return nil, errors.New("source template id and version are required")
		}
		if _, exists := seen[value.ID]; exists {
			return nil, fmt.Errorf("duplicate source template %q", value.ID)
		}
		seen[value.ID] = struct{}{}
		refs = append(refs, value)
	}
	return refs, nil
}

func validStepID(id string) bool {
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func validStepTransition(from, to string) bool {
	if from == to {
		return true
	}
	switch from {
	case StepPending:
		return to == StepInProgress || to == StepCompleted
	case StepInProgress:
		return to == StepCompleted
	default:
		return false
	}
}

func incompleteStepIDs(steps []TaskStep) []string {
	ids := make([]string, 0)
	for _, step := range steps {
		if step.Status != StepCompleted {
			ids = append(ids, step.ID)
		}
	}
	return ids
}

func normalizeStepIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeTexts(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		duplicate := false
		for _, existing := range out {
			if similarConditionText(existing, value) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func similarConditionText(a, b string) bool {
	a = conditionCompareText(a)
	b = conditionCompareText(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	aRuneLen := len([]rune(a))
	bRuneLen := len([]rune(b))
	if aRuneLen >= 8 && bRuneLen >= 8 && (strings.Contains(a, b) || strings.Contains(b, a)) {
		return true
	}
	if aRuneLen < 8 || bRuneLen < 8 {
		return false
	}
	aPairs := conditionBigramSet(a)
	bPairs := conditionBigramSet(b)
	if len(aPairs) == 0 || len(bPairs) == 0 {
		return false
	}
	shared := 0
	for pair := range aPairs {
		if _, ok := bPairs[pair]; ok {
			shared++
		}
	}
	return float64(shared*2)/float64(len(aPairs)+len(bPairs)) >= 0.62
}

func conditionCompareText(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		switch r {
		case '，', '。', '！', '？', '、', '；', '：', '（', '）', '(', ')', '[', ']', '【', '】', '{', '}', '《', '》', '“', '”', '‘', '’', '"', '\'', '`':
			continue
		}
		if r > ' ' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func conditionBigramSet(value string) map[string]struct{} {
	runes := []rune(value)
	out := map[string]struct{}{}
	if len(runes) < 2 {
		if value != "" {
			out[value] = struct{}{}
		}
		return out
	}
	for i := 0; i < len(runes)-1; i++ {
		out[string(runes[i:i+2])] = struct{}{}
	}
	return out
}

func requireMutable(task *Task) error {
	if task.Status == StatusCompleted {
		return errors.New("completed tasks are immutable")
	}
	return nil
}

func requireActive(task *Task) error {
	if task.Status != StatusActive {
		return fmt.Errorf("task status must be active, got %s", task.Status)
	}
	return nil
}

func requireFinalReviewOpen(task *Task) error {
	if task.FinalReview != nil && task.FinalReview.Status == FinalReviewPass {
		return errors.New("task already passed final review")
	}
	return nil
}

func normalizeReviewItems(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func appendTaskEvent(task *Task, event Event) {
	event.Summary = textutil.SafeTruncateString(strings.TrimSpace(event.Summary), maxTaskEventSummaryBytes).Text
	task.Events = append(task.Events, event)
	if len(task.Events) > maxTaskEvents {
		task.Events = append([]Event(nil), task.Events[len(task.Events)-maxTaskEvents:]...)
	}
}

func validateTextLimit(label, value string, maxBytes int) error {
	if len([]byte(value)) > maxBytes {
		return fmt.Errorf("%s exceeds %d bytes", label, maxBytes)
	}
	return nil
}

func validateID(id string) error {
	if !strings.HasPrefix(id, "tsk_") || len(id) != 20 {
		return errors.New("invalid task id")
	}
	for _, ch := range id[4:] {
		if !strings.ContainsRune("0123456789abcdef", ch) {
			return errors.New("invalid task id")
		}
	}
	return nil
}

func newID() (string, error) {
	return newOpaqueID("tsk_")
}

func newOpaqueID(prefix string) (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate %sid: %w", prefix, err)
	}
	return prefix + hex.EncodeToString(raw), nil
}
