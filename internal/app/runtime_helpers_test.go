package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/taskstate"
)

func newCodeToolsRuntime(t *testing.T) (*Runtime, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{
		AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock"),
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	server := newWorkflowTemplateNexusTestServer(t)
	rt.cfg.NexusEndpoint = server.URL
	rt.cfg.NexusDeviceToken = "test-device-token"
	return rt, root
}

func newWorkflowTemplateNexusTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	templates := map[string]taskstate.Template{}
	key := func(id, version string) string { return id + "@" + version }
	write := func(w http.ResponseWriter, payload map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}
	listSummaries := func(status taskstate.TemplateStatus) []map[string]any {
		items := []map[string]any{}
		for _, template := range templates {
			if status != "" && template.Status != status {
				continue
			}
			items = append(items, testCompactTemplateSummary(template))
		}
		return items
	}
	matchTemplates := func(goal, device, taskType string) []taskstate.TemplateCandidate {
		query := strings.ToLower(goal + " " + device + " " + taskType)
		candidates := []taskstate.TemplateCandidate{}
		for _, template := range templates {
			if template.Status != taskstate.TemplateActive {
				continue
			}
			score := 0
			reasons := []string{}
			for _, keyword := range template.Match.Keywords {
				if keyword != "" && strings.Contains(query, strings.ToLower(keyword)) {
					score += 15
					reasons = append(reasons, "keyword:"+keyword)
				}
			}
			if taskType != "" && template.Match.Type == taskType {
				score += 80
				reasons = append(reasons, "type:"+taskType)
			}
			if device != "" {
				for _, candidateDevice := range template.Match.Devices {
					if candidateDevice == device {
						score += 5
						reasons = append(reasons, "device:"+device)
					}
				}
			}
			if score > 0 {
				candidates = append(candidates, taskstate.TemplateCandidate{ID: template.ID, Version: template.Version, Score: score, Reason: strings.Join(reasons, ", ")})
			}
		}
		return candidates
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/workflow-templates", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		items := listSummaries(taskstate.TemplateStatus(r.URL.Query().Get("status")))
		write(w, map[string]any{"ok": true, "items": items, "templates": items, "count": len(items)})
	})
	mux.HandleFunc("/v1/workflow-templates/publish", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Template taskstate.Template `json:"template"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			write(w, map[string]any{"error": map[string]any{"message": err.Error()}})
			return
		}
		template := req.Template
		if _, exists := templates[key(template.ID, template.Version)]; exists {
			w.WriteHeader(http.StatusConflict)
			write(w, map[string]any{"error": map[string]any{"message": "published template version is immutable"}})
			return
		}
		for existingKey, existing := range templates {
			if existing.ID == template.ID && existing.Status == taskstate.TemplateActive {
				existing.Status = taskstate.TemplateRetired
				templates[existingKey] = existing
			}
		}
		template.Status = taskstate.TemplateActive
		template.Hash = "sha256:" + template.ID + "@" + template.Version
		template.RetiredAt = nil
		templates[key(template.ID, template.Version)] = template
		write(w, map[string]any{"ok": true, "template": template, "template_summary": testCompactTemplateSummary(template)})
	})
	mux.HandleFunc("/v1/workflow-templates/vector-index", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		write(w, map[string]any{"ok": true, "available": false, "vector_index_status": "missing"})
	})
	mux.HandleFunc("/v1/workflow-templates/match", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Goal, Device, Type string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		candidates := matchTemplates(req.Goal, req.Device, req.Type)
		result := map[string]any{"ok": true, "action": "match", "candidates": candidates, "count": len(candidates)}
		for key, value := range testTemplateMatchRecommendation(candidates) {
			result[key] = value
		}
		write(w, result)
	})
	mux.HandleFunc("/v1/workflow-templates/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/workflow-templates/"), "/"), "/")
		if len(parts) < 2 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id, version := parts[0], parts[1]
		template, ok := templates[key(id, version)]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			write(w, map[string]any{"error": map[string]any{"message": "template not found"}})
			return
		}
		if r.Method == http.MethodPost && len(parts) == 3 && parts[2] == "retire" {
			if template.Status != taskstate.TemplateActive {
				w.WriteHeader(http.StatusBadRequest)
				write(w, map[string]any{"error": map[string]any{"message": "template is not active"}})
				return
			}
			template.Status = taskstate.TemplateRetired
			templates[key(id, version)] = template
		} else if r.Method != http.MethodGet || len(parts) != 2 {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		write(w, map[string]any{"ok": true, "template": template, "template_summary": testCompactTemplateSummary(template)})
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-device-token" {
			t.Fatalf("Authorization = %q", got)
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func testTemplateMatchRecommendation(candidates []taskstate.TemplateCandidate) map[string]any {
	bestScore := 0
	if len(candidates) > 0 {
		bestScore = candidates[0].Score
	}
	recommended := "plain_task"
	reason := "no active template is specific enough; create a plain recoverable task"
	if bestScore >= 85 {
		recommended = "use_template"
		reason = "top candidate score is strong enough to select by default"
	} else if bestScore >= 60 {
		recommended = "consider_template"
		reason = "top candidate is plausible but should be checked against the user goal"
	}
	return map[string]any{
		"recommended": recommended, "recommendation_reason": reason, "best_candidate_score": bestScore,
		"score_thresholds": map[string]any{"use_template": 85, "consider_template": 60, "plain_task_below": 60},
	}
}

func testCompactTemplateSummary(template taskstate.Template) map[string]any {
	return map[string]any{
		"id": template.ID, "version": template.Version, "title": template.Title, "status": template.Status,
		"keyword_count": len(template.Match.Keywords), "device_count": len(template.Match.Devices), "type": template.Match.Type,
		"condition_count": len(template.CompletionConditions), "step_count": len(template.Steps),
		"allow_long_template": template.AllowLongTemplate, "long_template_reason": template.LongTemplateReason,
		"hash": template.Hash, "published_at": template.PublishedAt, "retired_at": template.RetiredAt,
	}
}
