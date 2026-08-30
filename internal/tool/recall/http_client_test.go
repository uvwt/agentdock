package recall

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/config"
)

func TestRecallClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Repeat("x", 2*1024*1024+1)))
	}))
	defer server.Close()

	service := newRecallClientTestService(server.URL, "test-device-token")
	_, err := service.request(context.Background(), http.MethodGet, "/v1/recall", nil)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "RECALL_RESPONSE_TOO_LARGE" {
		t.Fatalf("request() error = %v", err)
	}
}

func TestRecallClientPreservesCanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("canceled request reached server")
	}))
	defer server.Close()

	service := newRecallClientTestService(server.URL, "test-device-token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.request(ctx, http.MethodGet, "/v1/recall", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("request() error = %v, want context.Canceled", err)
	}
}

func TestRecallClientRequiresPairedDeviceToken(t *testing.T) {
	service := newRecallClientTestService("http://127.0.0.1:1", "")
	_, err := service.request(context.Background(), http.MethodGet, "/v1/recall", nil)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "RECALL_NOT_CONFIGURED" {
		t.Fatalf("request() error = %v", err)
	}
}

func newRecallClientTestService(endpoint, token string) *Service {
	return New(func() config.Config {
		return config.Config{NexusEndpoint: endpoint, NexusDeviceToken: token}
	})
}
