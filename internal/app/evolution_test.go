package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestTaskFinalReviewAutomaticallyResolvesPreboundLearningCheck(t *testing.T) {
	var mu sync.Mutex
	records := map[string]map[string]any{}
	nexus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/internal/recall/lifecycle/query":
			var query struct {
				Query       string   `json:"query"`
				EvolutionID string   `json:"evolution_id"`
				Statuses    []string `json:"statuses"`
			}
			if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
				t.Fatal(err)
			}
			items := make([]map[string]any, 0)
			for id, record := range records {
				if query.EvolutionID != "" && id != query.EvolutionID {
					continue
				}
				if len(query.Statuses) > 0 && !containsString(query.Statuses, record["status"]) {
					continue
				}
				if query.Query != "" && !strings.Contains(strings.ToLower(record["statement"].(string)), strings.ToLower(query.Query)) {
					continue
				}
				items = append(items, record)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"records": items, "count": len(items)})
		case "/internal/recall/lifecycle/transition":
			var request struct {
				ExpectedRevision int64          `json:"expected_revision"`
				Record           map[string]any `json:"record"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			id, _ := request.Record["evolution_id"].(string)
			currentRevision := int64(0)
			if current := records[id]; current != nil {
				currentRevision = int64(current["revision"].(float64))
			}
			if currentRevision != request.ExpectedRevision {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": "LIFECYCLE_REVISION_CONFLICT", "error": "stale"})
				return
			}
			request.Record["revision"] = float64(currentRevision + 1)
			records[id] = request.Record
			_ = json.NewEncoder(w).Encode(map[string]any{"record": request.Record})
		default:
			http.NotFound(w, r)
		}
	}))
	defer nexus.Close()

	home := t.TempDir()
	runtime, err := NewRuntime(config.Config{
		AgentDockHome: home, AgentDockDefaultDir: home,
		NexusEndpoint: nexus.URL, NexusDeviceToken: "device-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	proposed, err := runtime.Call(t.Context(), "evolve", map[string]any{
		"intent": "propose",
		"candidate": map[string]any{
			"type": "runbook", "statement": "final review 自动解析预绑定经验", "project": "agentdock",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchesOutputSchema(t, "evolve", proposed)
	evolutionID := proposed["evolution_id"].(string)

	created, err := runtime.Call(t.Context(), "task_manage", map[string]any{
		"action": "create", "title": "验证自动解析", "goal": "验证预绑定经验", "project": "agentdock",
		"completion_conditions": []string{"有真实结果"},
		"learning_checks": []map[string]any{{
			"evolution_id": evolutionID, "on_success": "support", "on_failure": "contradict",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID := created["task_id"].(string)
	final, err := runtime.Call(t.Context(), "task_manage", map[string]any{
		"action": "final_review", "task_id": taskID, "status": "pass", "summary": "真实验证通过",
		"verified": []string{"目标行为真实发生"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if warning, _ := final["evolution_warning"].(string); warning != "" {
		t.Fatalf("evolution warning = %q", warning)
	}

	mu.Lock()
	record := records[evolutionID]
	mu.Unlock()
	if record == nil || int(record["support_count"].(float64)) != 1 || record["status"] != "provisional" {
		t.Fatalf("resolved lifecycle record = %#v", record)
	}
}

func containsString(values []string, raw any) bool {
	want, _ := raw.(string)
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}

func TestRuntimeEvolveStageThreeIsProposalOnlyAndForcesProvisional(t *testing.T) {
	var stored map[string]any
	nexus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer device-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/internal/recall/lifecycle/query":
			_ = json.NewEncoder(w).Encode(map[string]any{"records": []any{}, "count": 0})
		case "/internal/recall/lifecycle/transition":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			stored, _ = request["record"].(map[string]any)
			stored["revision"] = float64(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"record": stored})
		default:
			http.NotFound(w, r)
		}
	}))
	defer nexus.Close()

	home := t.TempDir()
	runtime, err := NewRuntime(config.Config{
		AgentDockHome: home, AgentDockDefaultDir: home,
		NexusEndpoint: nexus.URL, NexusDeviceToken: "device-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	result, err := runtime.RuntimeEvolve(t.Context(), map[string]any{
		"intent":    "propose",
		"candidate": map[string]any{"type": "preference", "statement": "README 面向用户", "project": "agentdock", "source": "user-explicit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "provisional" {
		t.Fatalf("Stage 3 status = %#v, want provisional", result["status"])
	}
	if stored == nil || stored["source"] != "nexus-stage3" {
		t.Fatalf("stored record source = %#v", stored)
	}

	_, err = runtime.RuntimeEvolve(t.Context(), map[string]any{"intent": "assess", "evolution_id": result["evolution_id"]})
	if err == nil {
		t.Fatal("expected Stage 3 assess to be rejected")
	}
	toolErr, ok := err.(*ToolError)
	if !ok || toolErr.Code != "STAGE3_PROPOSAL_ONLY" {
		t.Fatalf("assess error = %#v", err)
	}

	_, err = runtime.evolve(t.Context(), map[string]any{"intent": "assess", "evolution_id": result["evolution_id"]})
	if err == nil {
		t.Fatal("expected model-facing assess to be rejected")
	}
	toolErr, ok = err.(*ToolError)
	if !ok || toolErr.Code != "INVALID_EVOLVE_INTENT" {
		t.Fatalf("model-facing assess error = %#v", err)
	}
}
