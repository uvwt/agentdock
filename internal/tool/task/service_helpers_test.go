package task

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/taskstate"
)

func newTaskTestService(t *testing.T) (*Service, string) {
	t.Helper()
	return newTaskTestServiceAt(t, t.TempDir())
}

func newTaskTestServiceAt(t *testing.T, root string) (*Service, string) {
	t.Helper()
	cfg := &config.Config{AgentDockDefaultDir: root, AgentDockHome: filepath.Join(root, ".agentdock")}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	tasks, err := taskstate.New(filepath.Join(cfg.AgentDockHome, "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(func() config.Config { return *cfg }, tasks)
	server := newWorkflowTemplateNexusTestServer(t)
	cfg.NexusEndpoint = server.URL
	cfg.NexusDeviceToken = "test-device-token"
	return service, root
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
			items = append(items, compactTemplateSummary(template))
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
		write(w, map[string]any{"ok": true, "template": template, "template_summary": compactTemplateSummary(template)})
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
		for key, value := range templateMatchRecommendation(candidates) {
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
		write(w, map[string]any{"ok": true, "template": template, "template_summary": compactTemplateSummary(template)})
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

func decodeTaskTestRequest[T any](input any) (T, error) {
	var request T
	if typed, ok := input.(T); ok {
		return typed, nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return request, err
	}
	return request, json.Unmarshal(data, &request)
}
func (s *Service) manageTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeTaskTestRequest[ManageRequest](input)
	if err != nil {
		return nil, err
	}
	return s.Manage(ctx, request)
}
func (s *Service) workflowManageTest(ctx context.Context, input any) (Result, error) {
	request, err := decodeTaskTestRequest[WorkflowRequest](input)
	if err != nil {
		return nil, err
	}
	return s.WorkflowManage(ctx, request)
}
