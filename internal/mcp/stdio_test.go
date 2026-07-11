package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/jsonrpc"
)

func TestServeStdioHandlesRequestsErrorsAndNotifications(t *testing.T) {
	server := &Server{}
	input := strings.Join([]string{
		`{"jsonrpc":`,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	}, "\n") + "\n"
	var output bytes.Buffer

	if err := server.ServeStdio(strings.NewReader(input), &output); err != nil {
		t.Fatalf("ServeStdio() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("response lines = %d, want 2; output=%q", len(lines), output.String())
	}

	var parseFailure map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lines[0]), &parseFailure); err != nil {
		t.Fatalf("decode parse failure: %v", err)
	}
	if got := string(parseFailure["id"]); got != "null" {
		t.Fatalf("parse failure id = %s, want null", got)
	}
	var rpcError jsonrpc.Error
	if err := json.Unmarshal(parseFailure["error"], &rpcError); err != nil {
		t.Fatalf("decode parse error object: %v", err)
	}
	if rpcError.Code != -32700 {
		t.Fatalf("parse error code = %d", rpcError.Code)
	}

	var pingResponse map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lines[1]), &pingResponse); err != nil {
		t.Fatalf("decode ping response: %v", err)
	}
	if got := string(pingResponse["id"]); got != "1" {
		t.Fatalf("ping id = %s, want 1", got)
	}
	if _, ok := pingResponse["result"]; !ok {
		t.Fatalf("ping response omitted result: %s", lines[1])
	}
}

func TestServeStdioReturnsResponseWriteFailure(t *testing.T) {
	server := &Server{}
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	err := server.ServeStdio(input, failingWriter{})
	if err == nil || !strings.Contains(err.Error(), "write JSON-RPC response") {
		t.Fatalf("ServeStdio() error = %v, want response write failure", err)
	}
}

func TestServeStdioReturnsParseErrorWriteFailure(t *testing.T) {
	server := &Server{}
	err := server.ServeStdio(strings.NewReader("{\n"), failingWriter{})
	if err == nil || !strings.Contains(err.Error(), "write parse error response") {
		t.Fatalf("ServeStdio() error = %v, want parse-error write failure", err)
	}
}

func TestDispatchProtocolAndMethodValidation(t *testing.T) {
	server := &Server{}
	invalidVersion := server.Dispatch(t.Context(), jsonrpc.Request{JSONRPC: "1.0", ID: 1, Method: "ping"})
	if invalidVersion.Error == nil || invalidVersion.Error.Code != -32600 {
		t.Fatalf("invalid version response = %#v", invalidVersion)
	}
	unknown := server.Dispatch(t.Context(), jsonrpc.Request{JSONRPC: "2.0", ID: "id", Method: "unknown"})
	if unknown.Error == nil || unknown.Error.Code != -32601 {
		t.Fatalf("unknown method response = %#v", unknown)
	}
	ping := server.Dispatch(t.Context(), jsonrpc.Request{JSONRPC: "2.0", ID: 0, Method: "ping"})
	if ping.Error != nil || ping.Result == nil {
		t.Fatalf("ping response = %#v", ping)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("writer failed")
}
