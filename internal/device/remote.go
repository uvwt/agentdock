package device

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/goal"
)

// RemoteClient talks to another AgentDock's runtime Goal API over HTTP.
// Used for multi-device handoff without a replicated database.
type RemoteClient struct {
	BaseURL    string
	AuthToken  string
	HTTPClient *http.Client
}

func (c *RemoteClient) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *RemoteClient) url(path string) string {
	return strings.TrimRight(c.BaseURL, "/") + path
}

func (c *RemoteClient) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	if c.AuthToken != "" {
		req.Header.Set("authorization", "Bearer "+c.AuthToken)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("remote %s %s: status %d: %s", method, path, resp.StatusCode, truncate(string(data), 400))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

// FetchGoal pulls full goal + capsule from a remote AgentDock.
func (c *RemoteClient) FetchGoal(ctx context.Context, goalID string) (goal.Goal, goal.Capsule, error) {
	var payload struct {
		Goal    goal.Goal    `json:"goal"`
		Capsule goal.Capsule `json:"capsule"`
	}
	if err := c.do(ctx, http.MethodGet, "/internal/runtime/goals/"+goalID, nil, &payload); err != nil {
		return goal.Goal{}, goal.Capsule{}, err
	}
	return payload.Goal, payload.Capsule, nil
}

// ListGoals lists remote goals.
func (c *RemoteClient) ListGoals(ctx context.Context, status string) ([]map[string]any, error) {
	path := "/internal/runtime/goals"
	if status != "" {
		path += "?status=" + status
	}
	var payload struct {
		Goals []map[string]any `json:"goals"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Goals, nil
}

// ImportCapsule creates a local goal from a remote capsule snapshot when only
// resume context is needed on another device. Full binary artifact sync is out of scope.
func ImportCapsule(store *goal.Store, cap goal.Capsule, objectiveFallback string) (goal.Goal, error) {
	if store == nil {
		return goal.Goal{}, fmt.Errorf("store is nil")
	}
	obj := cap.Objective
	if obj == "" {
		obj = objectiveFallback
	}
	if obj == "" {
		obj = cap.Title
	}
	criteria := make([]goal.SuccessCriterionInput, 0, len(cap.SuccessCriteria))
	for _, c := range cap.SuccessCriteria {
		criteria = append(criteria, goal.SuccessCriterionInput{ID: c.ID, Type: c.Type, Expression: c.Expression})
	}
	if len(criteria) == 0 {
		criteria = []goal.SuccessCriterionInput{{Type: goal.CriterionManual, Expression: "imported capsule"}}
	}
	ms := make([]goal.MilestoneInput, 0, len(cap.Milestones))
	for _, m := range cap.Milestones {
		ms = append(ms, goal.MilestoneInput{ID: m.ID, Title: m.Title})
	}
	g, err := store.Create(goal.CreateInput{
		Title:           firstNonEmpty(cap.Title, cap.GoalID, "imported-goal"),
		Objective:       obj,
		WorkspaceID:     cap.WorkspaceID,
		DeviceID:        cap.DeviceID,
		Mode:            cap.Mode,
		SuccessCriteria: criteria,
		Constraints:     cap.Constraints,
		Milestones:      ms,
		BaseGitSHA:      cap.BaseGitSHA,
	})
	if err != nil {
		return goal.Goal{}, err
	}
	// Stamp imported context without requiring a lease (local operator import).
	// Use mark blocked/resume path via fields by adding evidence notes.
	if cap.CurrentProblem != "" || cap.CurrentRequest != "" {
		_, _ = store.AddEvidence(g.ID, goal.EvidenceRef{
			Kind:    "import",
			Summary: "imported from remote capsule " + cap.GoalID,
			Data: map[string]any{
				"remote_goal_id":     cap.GoalID,
				"remote_capsule_ver": cap.CapsuleVersion,
				"current_problem":    cap.CurrentProblem,
				"current_request":    cap.CurrentRequest,
				"completed":          cap.Completed,
			},
		})
	}
	return store.Get(g.ID)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
