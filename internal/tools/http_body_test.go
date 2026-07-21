package tools

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadBoundedBody(t *testing.T) {
	data, err := readBoundedBody(strings.NewReader("exact"), 5)
	if err != nil {
		t.Fatalf("readBoundedBody() error = %v", err)
	}
	if string(data) != "exact" {
		t.Fatalf("data = %q", data)
	}
	if _, err := readBoundedBody(strings.NewReader("oversized"), 5); err == nil || !strings.Contains(err.Error(), "exceeds 5 bytes") {
		t.Fatalf("oversized error = %v", err)
	}
	if _, err := readBoundedBody(strings.NewReader("data"), 0); err == nil {
		t.Fatal("readBoundedBody() accepted zero limit")
	}
	if _, err := readBoundedBody(failingReader{}, 5); err == nil || !strings.Contains(err.Error(), "read response body") {
		t.Fatalf("reader error = %v", err)
	}
}

func TestNexusClientsRejectOversizedResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Repeat("x", 2*1024*1024+1)))
	}))
	defer server.Close()

	runtime, _ := newCodeToolsRuntime(t)
	runtime.cfg.NexusEndpoint = server.URL
	_, err := runtime.memoryRequest(context.Background(), http.MethodGet, "/v1/recall", nil)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "RECALL_RESPONSE_TOO_LARGE" {
		t.Fatalf("memoryRequest() error = %v", err)
	}
	_, err = runtime.nexusWorkflowJSON(context.Background(), http.MethodGet, "/v1/workflow-templates", nil)
	toolErr = nil
	if !errors.As(err, &toolErr) || toolErr.Code != "NEXUS_RESPONSE_BODY_INVALID" {
		t.Fatalf("nexusWorkflowJSON() error = %v", err)
	}
}

func TestNexusWorkflowJSONPreservesNonJSONHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 page not found"))
	}))
	defer server.Close()

	runtime, _ := newCodeToolsRuntime(t)
	runtime.cfg.NexusEndpoint = server.URL
	_, err := runtime.nexusWorkflowJSON(context.Background(), http.MethodGet, "/v1/workflow-templates/missing", nil)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "NEXUS_WORKFLOW_ERROR" {
		t.Fatalf("nexusWorkflowJSON() error = %v", err)
	}
	if toolErr.Message != "404 Not Found" {
		t.Fatalf("message = %q", toolErr.Message)
	}
	if toolErr.Details["status"] != http.StatusNotFound || toolErr.Details["response_preview"] != "404 page not found" {
		t.Fatalf("details = %#v", toolErr.Details)
	}
}

func TestNexusWorkflowJSONRejectsNonJSONSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	runtime, _ := newCodeToolsRuntime(t)
	runtime.cfg.NexusEndpoint = server.URL
	_, err := runtime.nexusWorkflowJSON(context.Background(), http.MethodGet, "/v1/workflow-templates", nil)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "NEXUS_INVALID_RESPONSE" {
		t.Fatalf("nexusWorkflowJSON() error = %v", err)
	}
	if toolErr.Details["response_preview"] != "not json" {
		t.Fatalf("details = %#v", toolErr.Details)
	}
}

func TestNexusClientsPreserveCanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("canceled request reached server")
	}))
	defer server.Close()

	runtime, _ := newCodeToolsRuntime(t)
	runtime.cfg.NexusEndpoint = server.URL
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtime.memoryRequest(ctx, http.MethodGet, "/v1/recall", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("memoryRequest() error = %v, want context.Canceled", err)
	}
	if _, err := runtime.nexusWorkflowJSON(ctx, http.MethodGet, "/v1/workflow-templates", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("nexusWorkflowJSON() error = %v, want context.Canceled", err)
	}
}

func TestGitHubGetRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 1<<20+1)))
	}))
	defer server.Close()

	_, _, status, message := githubGet(context.Background(), &http.Client{Timeout: time.Second}, "token", server.URL)
	if status != http.StatusOK || !strings.Contains(message, "response body exceeds") {
		t.Fatalf("githubGet() status=%d message=%q", status, message)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
