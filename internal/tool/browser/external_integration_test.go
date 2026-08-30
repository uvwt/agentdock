//go:build browser_integration

package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func startExternalCDPBrowser(t *testing.T) (string, func()) {
	t.Helper()
	executable := strings.TrimSpace(os.Getenv("AGENTDOCK_BROWSER_EXECUTABLE_PATH"))
	if executable == "" {
		t.Fatal("browser_integration requires AGENTDOCK_BROWSER_EXECUTABLE_PATH and must not skip")
	}
	profileDir, err := os.MkdirTemp("", "agentdock-external-cdp-")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable,
		"--headless=new",
		"--remote-debugging-port=0",
		"--user-data-dir="+profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"about:blank",
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		for range 20 {
			if err := os.RemoveAll(profileDir); err == nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		_ = os.RemoveAll(profileDir)
	}

	activePortPath := filepath.Join(profileDir, "DevToolsActivePort")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(activePortPath)
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) > 0 {
				port, parseErr := strconv.Atoi(strings.TrimSpace(lines[0]))
				if parseErr == nil && port > 0 {
					endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
					if probeCDPEndpoint(context.Background(), endpoint) == nil {
						return endpoint, cleanup
					}
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	cleanup()
	t.Fatalf("external Chrome did not publish %s", activePortPath)
	return "", func() {}
}

func TestExternalCDPAttachKeepsBrowserAliveAndIsolatesTargets(t *testing.T) {
	endpoint, cleanup := startExternalCDPBrowser(t)
	defer cleanup()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `<title>External CDP</title><main id="ready">external-ready</main>`)
	}))
	defer server.Close()

	service := New(Config{AgentDockHome: t.TempDir()}, nil)
	started, err := service.start(context.Background(), StartRequest{
		CDPURL:  endpoint,
		URL:     server.URL,
		Timeout: 15 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.ConnectionMode != "external_explicit" {
		t.Fatalf("connection mode = %q", started.ConnectionMode)
	}
	if len(started.Pages) != 1 {
		t.Fatalf("external session exposed %d pages, want only AgentDock target: %#v", len(started.Pages), started.Pages)
	}
	if _, err := service.act(context.Background(), ActRequest{
		SessionID: started.SessionID,
		Actions:   []Action{{Kind: "wait_for_text", WaitText: &WaitTextAction{Text: "external-ready", Exact: true, State: StateVisible, Timeout: 5 * time.Second}}},
		Timeout:   10 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.closeSession(CloseRequest{SessionID: started.SessionID}); err != nil {
		t.Fatal(err)
	}
	if err := probeCDPEndpoint(context.Background(), endpoint); err != nil {
		t.Fatalf("external browser stopped after AgentDock session close: %v", err)
	}

	candidates, err := discoverCDPEndpoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range candidates {
		if candidate.URL == endpoint {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("auto-discovery did not find random-port external browser %s: %#v", endpoint, candidates)
	}
}
