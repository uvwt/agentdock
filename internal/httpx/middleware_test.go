package httpx

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatusRecorderKeepsFirstStatusAndCountsBytes(t *testing.T) {
	underlying := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: underlying}
	recorder.WriteHeader(http.StatusCreated)
	recorder.WriteHeader(http.StatusInternalServerError)
	written, err := recorder.Write([]byte("payload"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if written != len("payload") || recorder.bytes != len("payload") {
		t.Fatalf("written=%d recorded=%d", written, recorder.bytes)
	}
	if recorder.status != http.StatusCreated || underlying.Code != http.StatusCreated {
		t.Fatalf("recorded status=%d underlying status=%d, want %d", recorder.status, underlying.Code, http.StatusCreated)
	}
	if recorder.Unwrap() != underlying {
		t.Fatal("Unwrap() did not return underlying writer")
	}
}

func TestStatusRecorderInfersOKOnWrite(t *testing.T) {
	underlying := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: underlying}
	if _, err := recorder.Write([]byte("ok")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if recorder.status != http.StatusOK || underlying.Code != http.StatusOK {
		t.Fatalf("recorded status=%d underlying status=%d", recorder.status, underlying.Code)
	}
}

func TestLoggingMiddlewareDoesNotLogHeadersOrBody(t *testing.T) {
	previous := slog.Default()
	defer slog.SetDefault(previous)
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))

	handler := loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("response-secret"))
	}))
	request := httptest.NewRequest(http.MethodPost, "/mcp?code=query-secret", strings.NewReader("request-secret"))
	request.Header.Set("Authorization", "Bearer header-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	logText := logs.String()
	for _, secret := range []string{"query-secret", "request-secret", "response-secret", "header-secret"} {
		if strings.Contains(logText, secret) {
			t.Fatalf("log leaked %q: %s", secret, logText)
		}
	}
	for _, expected := range []string{`"method":"POST"`, `"path":"/mcp"`, `"status":202`, `"bytes":15`} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("log missing %s: %s", expected, logText)
		}
	}
}
