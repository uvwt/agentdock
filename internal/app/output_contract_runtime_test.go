package app

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	acpruntime "github.com/uvwt/agentdock/internal/acp"
	"github.com/uvwt/agentdock/internal/config"
)

func assertToolResultMatchesOutputSchema(t *testing.T, name string, result Result) map[string]any {
	t.Helper()

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal %s result: %v", name, err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		t.Fatalf("unmarshal %s result: %v", name, err)
	}

	schemaJSON, err := json.Marshal(OutputSchema(name))
	if err != nil {
		t.Fatalf("marshal %s output schema: %v", name, err)
	}
	var schema any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		t.Fatalf("unmarshal %s output schema: %v", name, err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	resource := "urn:agentdock:test:output:" + name
	if err := compiler.AddResource(resource, schema); err != nil {
		t.Fatalf("load %s output schema: %v", name, err)
	}
	compiled, err := compiler.Compile(resource)
	if err != nil {
		t.Fatalf("compile %s output schema: %v", name, err)
	}
	if err := compiled.Validate(normalized); err != nil {
		t.Fatalf("%s result violates output schema: %v\nresult: %s", name, err, encoded)
	}
	return normalized
}

func TestRuntimeOutputContractDefaultToolSuccessPaths(t *testing.T) {
	runtime := newRuntimeValidationTestRuntime(t)
	root := runtime.Config().AgentDockDefaultDir
	if err := os.WriteFile(filepath.Join(root, "contract.txt"), []byte("contract-marker\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	calls := []struct {
		name string
		args map[string]any
	}{
		{name: "agentdock_context", args: map[string]any{}},
		{name: "read_file", args: map[string]any{"path": "contract.txt"}},
		{name: "list_dir", args: map[string]any{"path": "."}},
		{name: "search_text", args: map[string]any{"path": ".", "query": "contract-marker"}},
		{name: "file_edit", args: map[string]any{"action": "replace", "path": "contract.txt", "old": "contract-marker", "new": "updated-marker", "dry_run": true}},
		{name: "exec_command", args: map[string]any{"cmd": "printf contract-marker", "execution_mode": "sync"}},
		{name: "session_observe", args: map[string]any{"action": "list"}},
		{name: "session_act", args: map[string]any{"action": "kill_all"}},
		{name: "task_manage", args: map[string]any{"action": "list"}},
		{name: "mcp_manage", args: map[string]any{"action": "list"}},
		{name: "mcp_tool_search", args: map[string]any{"query": "*"}},
		{name: "file_publish", args: map[string]any{"path": "contract.txt", "retention_seconds": 60}},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			result, err := runtime.Call(context.Background(), call.name, call.args)
			if err != nil {
				t.Fatal(err)
			}
			assertToolResultMatchesOutputSchema(t, call.name, result)
		})
	}
}

func TestRuntimeOutputContractRecallReadBootstrapMaintain(t *testing.T) {
	store := map[string]string{
		"devices/test.md": "---\ntype: test\n---\n\n# Contract\n",
	}
	runtime, closeServer := newMemoryTestRuntime(t, store)
	defer closeServer()
	defer runtime.Close()

	calls := []struct {
		name string
		args map[string]any
	}{
		{name: "recall_read", args: map[string]any{"path": "devices/test.md"}},
		{name: "recall_bootstrap", args: map[string]any{}},
		{name: "recall_maintain", args: map[string]any{"action": "list"}},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			result, err := runtime.Call(context.Background(), call.name, call.args)
			if err != nil {
				t.Fatal(err)
			}
			assertToolResultMatchesOutputSchema(t, call.name, result)
		})
	}
}

func TestRuntimeOutputContractACPInfoNormalizesOmittedInitializeFields(t *testing.T) {
	const helperEnv = "GO_WANT_OUTPUT_CONTRACT_ACP_HELPER"
	const omitCapabilitiesEnv = "GO_OUTPUT_CONTRACT_ACP_OMIT_CAPABILITIES"
	t.Setenv(helperEnv, "1")
	t.Setenv(omitCapabilitiesEnv, "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := config.Config{
		AgentDockHome:       filepath.Join(root, ".agentdock"),
		AgentDockDefaultDir: root,
		ACPEnabled:          true,
		ACPAgentName:        "output-contract-helper",
		ACPCommand:          executable,
		ACPArgs:             []string{"-test.run=^TestOutputContractACPHelper$"},
		ACPEnvFromEnv: map[string]string{
			helperEnv:           helperEnv,
			omitCapabilitiesEnv: omitCapabilitiesEnv,
		},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Close() }()

	info, err := runtime.Call(context.Background(), "acp_session", map[string]any{"action": "info"})
	if err != nil {
		t.Fatal(err)
	}
	normalized := assertToolResultMatchesOutputSchema(t, "acp_session", info)
	capabilities, ok := normalized["capabilities"].(map[string]any)
	if !ok || len(capabilities) != 0 {
		t.Fatalf("omitted ACP capabilities = %#v, want {}", normalized["capabilities"])
	}
	authMethods, ok := normalized["auth_methods"].([]any)
	if !ok || len(authMethods) != 0 {
		t.Fatalf("omitted ACP auth_methods = %#v, want []", normalized["auth_methods"])
	}
}

func TestRuntimeOutputContractACPOptionalFields(t *testing.T) {
	const helperEnv = "GO_WANT_OUTPUT_CONTRACT_ACP_HELPER"
	t.Setenv(helperEnv, "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg := config.Config{
		AgentDockHome:       filepath.Join(root, ".agentdock"),
		AgentDockDefaultDir: root,
		ACPEnabled:          true,
		ACPAgentName:        "output-contract-helper",
		ACPCommand:          executable,
		ACPArgs:             []string{"-test.run=^TestOutputContractACPHelper$"},
		ACPEnvFromEnv:       map[string]string{helperEnv: helperEnv},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Close() }()

	info, err := runtime.Call(context.Background(), "acp_session", map[string]any{"action": "info"})
	if err != nil {
		t.Fatal(err)
	}
	normalizedInfo := assertToolResultMatchesOutputSchema(t, "acp_session", info)
	authMethods, ok := normalizedInfo["auth_methods"].([]any)
	if !ok || len(authMethods) != 0 {
		t.Fatalf("auth_methods = %#v, want []", normalizedInfo["auth_methods"])
	}

	listed, err := runtime.Call(context.Background(), "acp_session", map[string]any{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	normalizedListed := assertToolResultMatchesOutputSchema(t, "acp_session", listed)
	sessions, ok := normalizedListed["sessions"].([]any)
	if !ok || len(sessions) != 0 {
		t.Fatalf("empty ACP sessions = %#v, want []", normalizedListed["sessions"])
	}

	interactions, err := runtime.Call(context.Background(), "acp_interaction", map[string]any{"action": "list"})
	if err != nil {
		t.Fatal(err)
	}
	normalizedInteractions := assertToolResultMatchesOutputSchema(t, "acp_interaction", interactions)
	pending, ok := normalizedInteractions["interactions"].([]any)
	if !ok || len(pending) != 0 {
		t.Fatalf("empty ACP interactions = %#v, want []", normalizedInteractions["interactions"])
	}

	created, err := runtime.Call(context.Background(), "acp_session", map[string]any{"action": "new"})
	if err != nil {
		t.Fatal(err)
	}
	assertACPOptionalSessionFieldsAbsent(t, created)
	session, ok := created["session"].(acpruntime.SessionRecord)
	if !ok || session.ID == "" {
		t.Fatalf("created session = %#v", created["session"])
	}

	for _, action := range []string{"load", "resume"} {
		result, callErr := runtime.Call(context.Background(), "acp_session", map[string]any{"action": action, "session_id": session.ID})
		if callErr != nil {
			t.Fatalf("%s: %v", action, callErr)
		}
		assertACPOptionalSessionFieldsAbsent(t, result)
	}

	forked, err := runtime.Call(context.Background(), "acp_session", map[string]any{"action": "fork", "session_id": session.ID})
	if err != nil {
		t.Fatal(err)
	}
	assertACPOptionalSessionFieldsAbsent(t, forked)

	configured, err := runtime.Call(context.Background(), "acp_session", map[string]any{
		"action": "set_config", "session_id": session.ID, "config_id": "safe", "config_value": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	normalizedConfigured := assertToolResultMatchesOutputSchema(t, "acp_session", configured)
	configOptions, ok := normalizedConfigured["config_options"].([]any)
	if !ok || len(configOptions) != 0 {
		t.Fatalf("set_config config_options = %#v, want []", normalizedConfigured["config_options"])
	}

	started, err := runtime.Call(context.Background(), "acp_prompt", map[string]any{"action": "start", "session_id": session.ID, "text": "hold"})
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchesOutputSchema(t, "acp_prompt", started)
	runID, _ := started["run_id"].(string)
	if runID == "" {
		t.Fatalf("prompt start = %#v", started)
	}
	events, err := runtime.Call(context.Background(), "acp_prompt", map[string]any{"action": "events", "run_id": runID})
	if err != nil {
		t.Fatal(err)
	}
	normalizedEvents := assertToolResultMatchesOutputSchema(t, "acp_prompt", events)
	if normalizedEvents["status"] != "running" {
		t.Fatalf("prompt status = %#v, want running", normalizedEvents["status"])
	}
	if _, exists := normalizedEvents["ended_at"]; exists {
		t.Fatalf("running prompt returned ended_at: %#v", normalizedEvents["ended_at"])
	}

	nextSeq, ok := events["next_seq"].(uint64)
	if !ok {
		t.Fatalf("prompt next_seq = %#v", events["next_seq"])
	}
	settled, err := runtime.Call(context.Background(), "acp_prompt", map[string]any{
		"action": "events", "run_id": runID, "after_seq": int(nextSeq), "wait_ms": 3000,
	})
	if err != nil {
		t.Fatal(err)
	}
	normalizedSettled := assertToolResultMatchesOutputSchema(t, "acp_prompt", settled)
	if normalizedSettled["status"] != "completed" {
		t.Fatalf("settled prompt status = %#v, want completed", normalizedSettled["status"])
	}
	endedAt, ok := normalizedSettled["ended_at"].(string)
	if !ok || endedAt == "" {
		t.Fatalf("settled prompt ended_at = %#v, want RFC3339 string", normalizedSettled["ended_at"])
	}
}

func assertACPOptionalSessionFieldsAbsent(t *testing.T, result Result) {
	t.Helper()
	normalized := assertToolResultMatchesOutputSchema(t, "acp_session", result)
	for _, field := range []string{"modes", "config_options"} {
		if _, exists := normalized[field]; exists {
			t.Fatalf("optional %s should be omitted: %#v", field, normalized[field])
		}
	}
}

func TestOutputContractACPHelper(t *testing.T) {
	if os.Getenv("GO_WANT_OUTPUT_CONTRACT_ACP_HELPER") != "1" {
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	remoteSession := 0
	for scanner.Scan() {
		var request struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(2)
		}

		var result any
		switch request.Method {
		case "initialize":
			initialize := map[string]any{
				"protocolVersion": acpruntime.ProtocolVersion,
				"agentInfo":       map[string]any{"name": "output-contract-helper", "version": "1.0.0"},
			}
			if os.Getenv("GO_OUTPUT_CONTRACT_ACP_OMIT_CAPABILITIES") != "1" {
				initialize["agentCapabilities"] = map[string]any{
					"loadSession": true,
					"sessionCapabilities": map[string]any{
						"resume": map[string]any{}, "fork": map[string]any{},
					},
				}
			}
			result = initialize
		case "session/new", "session/fork":
			remoteSession++
			result = map[string]any{"sessionId": "remote-" + strconv.Itoa(remoteSession)}
		case "session/load", "session/resume":
			result = map[string]any{}
		case "session/set_config_option":
			result = map[string]any{"configOptions": []any{}}
		case "session/prompt":
			time.Sleep(2 * time.Second)
			result = map[string]any{"stopReason": "end_turn"}
		default:
			if request.ID == nil {
				continue
			}
			_ = encoder.Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"error": map[string]any{"code": -32601, "message": "Method not found"},
			})
			continue
		}
		if request.ID != nil {
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		}
	}
	os.Exit(0)
}
