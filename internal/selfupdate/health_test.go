package selfupdate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitForVersionRequiresConsecutiveHealthyResponses(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		version := "v0.4.5"
		if call == 2 {
			version = "v0.4.4"
		}
		_ = json.NewEncoder(w).Encode(healthResponse{OK: true, Version: version})
	}))
	defer server.Close()

	if err := waitForVersion(context.Background(), []string{server.URL}, "v0.4.5", 4*time.Second); err != nil {
		t.Fatal(err)
	}
	if calls.Load() < 4 {
		t.Fatalf("health check returned before two consecutive successes: %d calls", calls.Load())
	}
}

func TestHealthCandidatesReadsPortFromDefaultMacStartScript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AGENTDOCK_PORT", "")

	runtimeDir := filepath.Join(home, "agentdock-runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	startScript := []byte("exec \"$HOME/agentdock/agentdock\" --host 127.0.0.1 --port 18766\n")
	if err := os.WriteFile(filepath.Join(runtimeDir, "start-agentdock.sh"), startScript, 0o700); err != nil {
		t.Fatal(err)
	}

	candidates := healthCandidates(filepath.Join(home, "agentdock", "agentdock"))
	want := "http://127.0.0.1:18766/healthz"
	if !slices.Contains(candidates, want) {
		t.Fatalf("health candidates %v do not contain %s", candidates, want)
	}
}
