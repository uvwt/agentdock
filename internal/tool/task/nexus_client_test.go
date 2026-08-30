package task

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestNexusWorkflowClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Repeat("x", 2*1024*1024+1)))
	}))
	defer server.Close()

	service := newNexusWorkflowClientTestService(server.URL, "test-device-token")
	_, err := service.nexusWorkflowJSON(context.Background(), http.MethodGet, "/v1/workflow-templates", nil)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "NEXUS_RESPONSE_BODY_INVALID" {
		t.Fatalf("nexusWorkflowJSON() error = %v", err)
	}
}

func TestNexusWorkflowClientPreservesNonJSONHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 page not found"))
	}))
	defer server.Close()

	service := newNexusWorkflowClientTestService(server.URL, "test-device-token")
	_, err := service.nexusWorkflowJSON(context.Background(), http.MethodGet, "/v1/workflow-templates/missing", nil)
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

func TestNexusWorkflowClientRejectsNonJSONSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	service := newNexusWorkflowClientTestService(server.URL, "test-device-token")
	_, err := service.nexusWorkflowJSON(context.Background(), http.MethodGet, "/v1/workflow-templates", nil)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "NEXUS_INVALID_RESPONSE" {
		t.Fatalf("nexusWorkflowJSON() error = %v", err)
	}
	if toolErr.Details["response_preview"] != "not json" {
		t.Fatalf("details = %#v", toolErr.Details)
	}
}

func TestNexusWorkflowClientPreservesCanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("canceled request reached server")
	}))
	defer server.Close()

	service := newNexusWorkflowClientTestService(server.URL, "test-device-token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.nexusWorkflowJSON(ctx, http.MethodGet, "/v1/workflow-templates", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("nexusWorkflowJSON() error = %v, want context.Canceled", err)
	}
}

func TestNexusWorkflowClientRequiresPairedDeviceToken(t *testing.T) {
	service := newNexusWorkflowClientTestService("http://127.0.0.1:1", "")
	_, err := service.nexusWorkflowJSON(context.Background(), http.MethodGet, "/v1/workflow-templates", nil)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "NEXUS_NOT_CONFIGURED" {
		t.Fatalf("nexusWorkflowJSON() error = %v", err)
	}
}

func newNexusWorkflowClientTestService(endpoint, token string) *Service {
	return New(func() config.Config {
		return config.Config{NexusEndpoint: endpoint, NexusDeviceToken: token}
	}, nil)
}
