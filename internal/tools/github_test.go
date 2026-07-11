package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGitHubGetDecodesBoundedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-value" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("X-OAuth-Scopes", "repo, read:user")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"agentdock-user","message":"ok"}`))
	}))
	defer server.Close()

	client := &http.Client{Timeout: time.Second}
	login, scopes, status, message := githubGet(context.Background(), client, "token-value", server.URL)
	if login != "agentdock-user" || scopes != "repo, read:user" || status != http.StatusOK || message != "ok" {
		t.Fatalf("githubGet() = login=%q scopes=%q status=%d message=%q", login, scopes, status, message)
	}
}

func TestGitHubGetReportsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>proxy error</html>"))
	}))
	defer server.Close()

	login, _, status, message := githubGet(context.Background(), &http.Client{Timeout: time.Second}, "token-value", server.URL)
	if login != "" || status != http.StatusBadGateway || !strings.Contains(message, "decode GitHub response") {
		t.Fatalf("githubGet() = login=%q status=%d message=%q", login, status, message)
	}
}

func TestGitHubGetHonorsCanceledContext(t *testing.T) {
	reached := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached <- struct{}{}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, status, message := githubGet(ctx, &http.Client{Timeout: time.Second}, "token-value", server.URL)
	if status != 0 || !strings.Contains(message, context.Canceled.Error()) {
		t.Fatalf("githubGet() status=%d message=%q, want canceled request", status, message)
	}
	select {
	case <-reached:
		t.Fatal("canceled GitHub request reached server")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestGitHubGetAllowsEmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	login, scopes, status, message := githubGet(context.Background(), &http.Client{Timeout: time.Second}, "token-value", server.URL)
	if login != "" || scopes != "" || status != http.StatusNoContent || message != "" {
		t.Fatalf("githubGet() = login=%q scopes=%q status=%d message=%q", login, scopes, status, message)
	}
}
