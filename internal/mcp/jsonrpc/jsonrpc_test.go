package jsonrpc

import (
	"encoding/json"
	"testing"
)

func TestSuccessAlwaysEncodesIDAndResult(t *testing.T) {
	tests := []struct {
		name   string
		id     any
		result any
	}{
		{name: "null values"},
		{name: "zero id", id: 0, result: map[string]any{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(Success(test.id, test.result))
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &payload); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if _, ok := payload["id"]; !ok {
				t.Fatalf("response omitted id: %s", encoded)
			}
			if _, ok := payload["result"]; !ok {
				t.Fatalf("response omitted result: %s", encoded)
			}
			if _, ok := payload["error"]; ok {
				t.Fatalf("success response included error: %s", encoded)
			}
			if string(payload["jsonrpc"]) != `"2.0"` {
				t.Fatalf("jsonrpc = %s", payload["jsonrpc"])
			}
		})
	}
}

func TestFailureEncodesNullIDWithoutResult(t *testing.T) {
	encoded, err := json.Marshal(Failure(nil, -32700, "Parse error", nil))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got := string(payload["id"]); got != "null" {
		t.Fatalf("id = %s, want null; response=%s", got, encoded)
	}
	if _, ok := payload["error"]; !ok {
		t.Fatalf("response omitted error: %s", encoded)
	}
	if _, ok := payload["result"]; ok {
		t.Fatalf("failure response included result: %s", encoded)
	}
	var rpcError Error
	if err := json.Unmarshal(payload["error"], &rpcError); err != nil {
		t.Fatalf("decode error object: %v", err)
	}
	if rpcError.Code != -32700 || rpcError.Message != "Parse error" {
		t.Fatalf("error = %#v", rpcError)
	}
}

func TestFailurePreservesErrorData(t *testing.T) {
	encoded, err := json.Marshal(Failure("request-1", -32602, "Invalid params", map[string]any{"field": "name"}))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var payload struct {
		ID    string `json:"id"`
		Error Error  `json:"error"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.ID != "request-1" {
		t.Fatalf("id = %q", payload.ID)
	}
	data, ok := payload.Error.Data.(map[string]any)
	if !ok || data["field"] != "name" {
		t.Fatalf("error data = %#v", payload.Error.Data)
	}
}
