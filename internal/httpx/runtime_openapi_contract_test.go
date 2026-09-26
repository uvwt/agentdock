package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	runtimecontractv1 "github.com/uvwt/agentdock-protocol/runtimecontract/v1"
	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/auth"
)

func TestRuntimeHTTPReadResponsesMatchOpenAPIContractV1(t *testing.T) {
	cfg := testConfig(t)
	skillRoot := filepath.Join(cfg.AgentDockDefaultDir, "demo-skill")
	if err := os.MkdirAll(skillRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("---\nname: demo-skill\ndescription: Runtime contract fixture.\n---\n\n# Demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	if _, err := runtime.Call(context.Background(), "skill_manage", map[string]any{"action": "install", "source": "demo-skill"}); err != nil {
		t.Fatalf("install contract Skill: %v", err)
	}
	created, err := runtime.Call(context.Background(), "task_manage", map[string]any{
		"action":                "create",
		"title":                 "Runtime contract task",
		"goal":                  "Keep Runtime wire responses inside OpenAPI v1",
		"steps":                 []map[string]any{{"id": "verify", "title": "Verify contract"}},
		"completion_conditions": []string{"Runtime response validates against v1"},
	})
	if err != nil {
		t.Fatalf("create contract task: %v", err)
	}
	taskID, _ := created["task_id"].(string)
	if taskID == "" {
		t.Fatalf("created task missing task_id: %#v", created)
	}

	loader := openapi3.NewLoader()
	document, err := loader.LoadFromData([]byte(runtimecontractv1.OpenAPI()))
	if err != nil {
		t.Fatalf("load Runtime OpenAPI v1: %v", err)
	}
	if err := document.Validate(context.Background()); err != nil {
		t.Fatalf("validate Runtime OpenAPI v1: %v", err)
	}

	handler := runtimeAPIHandler(runtime, cfg, auth.NewOAuthStore())
	cases := []struct {
		name   string
		path   string
		schema string
	}{
		{name: "status", path: "/internal/runtime/status", schema: "StatusResponse"},
		{name: "tasks", path: "/internal/runtime/tasks", schema: "TaskListResponse"},
		{name: "task detail", path: "/internal/runtime/tasks/" + taskID, schema: "TaskDetailResponse"},
		{name: "skills", path: "/internal/runtime/skills", schema: "SkillListResponse"},
		{name: "skill detail", path: "/internal/runtime/skills/demo-skill", schema: "SkillDetailResponse"},
		{name: "skill files", path: "/internal/runtime/skills/demo-skill/files", schema: "SkillFilesResponse"},
		{name: "skill file", path: "/internal/runtime/skills/demo-skill/files/SKILL.md", schema: "SkillFileResponse"},
		{name: "plugins", path: "/internal/runtime/plugins", schema: "PluginListResponse"},
		{name: "mcp", path: "/internal/runtime/mcp", schema: "MCPListResponse"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
			}
			var payload any
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode Runtime response: %v", err)
			}
			schema := document.Components.Schemas[test.schema]
			if schema == nil || schema.Value == nil {
				t.Fatalf("OpenAPI schema %s is missing", test.schema)
			}
			if err := schema.Value.VisitJSON(payload); err != nil {
				t.Fatalf("response violates Runtime contract v1 schema %s: %v\n%s", test.schema, err, recorder.Body.String())
			}
		})
	}
}
