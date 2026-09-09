package browser

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateCDPURL(t *testing.T) {
	for _, value := range []string{"http://127.0.0.1:9222", "https://browser.example", "ws://127.0.0.1/devtools/browser/id", "wss://browser.example/devtools/browser/id"} {
		if err := validateCDPURL(value); err != nil {
			t.Fatalf("validateCDPURL(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"", "127.0.0.1:9222", "file:///tmp/socket", "ftp://127.0.0.1:9222"} {
		if err := validateCDPURL(value); err == nil {
			t.Fatalf("validateCDPURL(%q) unexpectedly succeeded", value)
		}
	}
}

func TestValidateToolCDPURLRequiresLoopback(t *testing.T) {
	for _, value := range []string{"http://127.0.0.1:9222", "ws://[::1]:9222/devtools/browser/id"} {
		if err := validateToolCDPURL(value); err != nil {
			t.Fatalf("validateToolCDPURL(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"http://localhost:9222", "http://192.168.1.10:9222", "https://browser.example:9222", "ws://10.0.0.2:9222/devtools/browser/id"} {
		if err := validateToolCDPURL(value); err == nil {
			t.Fatalf("validateToolCDPURL(%q) unexpectedly succeeded", value)
		}
	}
}

func TestDirectHTTPClientDisablesProxy(t *testing.T) {
	transport, ok := directHTTPClient(time.Second).Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatalf("direct HTTP transport = %#v, want proxy disabled", transport)
	}
}

func TestDirectHTTPClientRejectsRedirects(t *testing.T) {
	targetHits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	resp, err := directHTTPClient(time.Second).Get(redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if targetHits != 0 {
		t.Fatalf("redirect target received %d requests, want 0", targetHits)
	}
}

func TestResolveCDPWebSocket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/test"}`))
	}))
	defer server.Close()

	got, err := resolveCDPWebSocket(context.Background(), server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	want := "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/browser/test"
	if got != want {
		t.Fatalf("websocket = %q, want %q", got, want)
	}
}

func TestResolveCDPWebSocketPinsEndpointHost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://169.254.169.254:9222/devtools/browser/test"}`))
	}))
	defer server.Close()

	got, err := resolveCDPWebSocket(context.Background(), server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	want := "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/browser/test"
	if got != want {
		t.Fatalf("websocket = %q, want pinned host %q", got, want)
	}
}

func TestExtractUserDataDir(t *testing.T) {
	cases := map[string]string{
		`chrome --user-data-dir=/tmp/a --remote-debugging-port=9222`:                                                         "/tmp/a",
		`chrome --user-data-dir="/tmp/with space" --remote-debugging-port=0`:                                                 "/tmp/with space",
		`chrome --user-data-dir '/tmp/single space'`:                                                                         "/tmp/single space",
		`msedge.exe "--user-data-dir=C:\Users\Administrator\AppData\Local\Microsoft\Edge\User Data" --type=crashpad-handler`: `C:\Users\Administrator\AppData\Local\Microsoft\Edge\User Data`,
	}
	for command, want := range cases {
		if got := extractUserDataDir(command); got != want {
			t.Fatalf("extractUserDataDir(%q) = %q, want %q", command, got, want)
		}
	}
}

func TestCandidateFromDevToolsActivePort(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{name: "browser websocket", data: "49152\n/devtools/browser/test\n", want: "ws://127.0.0.1:49152/devtools/browser/test"},
		{name: "single line fallback", data: "49152\n", want: "http://127.0.0.1:49152"},
		{name: "absolute URL fallback", data: "49152\nws://169.254.169.254/devtools/browser/test\n", want: "http://127.0.0.1:49152"},
		{name: "network path fallback", data: "49152\n//example.invalid/devtools/browser/test\n", want: "http://127.0.0.1:49152"},
		{name: "query fallback", data: "49152\n/devtools/browser/test?token=unexpected\n", want: "http://127.0.0.1:49152"},
		{name: "nested path fallback", data: "49152\n/devtools/browser/test/extra\n", want: "http://127.0.0.1:49152"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "DevToolsActivePort"), []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			candidate, ok := candidateFromDevToolsActivePort(dir)
			if !ok || candidate.URL != tc.want || candidate.Source != "devtools_active_port" {
				t.Fatalf("candidate = %#v, ok=%v, want URL %q", candidate, ok, tc.want)
			}
		})
	}
}

func TestProbeCDPEndpointAcceptsBrowserWebSocketListenerWithoutHTTPDiscovery(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "ws://" + listener.Addr().String() + "/devtools/browser/test"
	if err := probeCDPEndpoint(context.Background(), endpoint); err != nil {
		listener.Close()
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := probeCDPEndpoint(context.Background(), endpoint); err == nil {
		t.Fatal("closed browser websocket listener unexpectedly passed probe")
	}
}

func TestProbeCDPEndpointRequiresBrowserWebSocket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://127.0.0.1/devtools/browser/test"}`))
	}))
	defer server.Close()
	if err := probeCDPEndpoint(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
}

func TestResolveCDPConnectionTrustBoundary(t *testing.T) {
	service := New(Config{CDPURL: "http://browser.internal:9222"}, nil)
	got, mode, err := service.resolveCDPConnection(context.Background(), StartRequest{})
	if err != nil || got != "http://browser.internal:9222" || mode != "external_configured" {
		t.Fatalf("configured remote CDP = url %q mode %q err %v", got, mode, err)
	}

	service = New(Config{}, nil)
	_, _, err = service.resolveCDPConnection(context.Background(), StartRequest{CDPURL: "http://browser.internal:9222"})
	var browserErr *Error
	if !errors.As(err, &browserErr) || browserErr.Code != ErrActionInvalid {
		t.Fatalf("tool remote CDP error = %#v", err)
	}
}

func TestResolveCDPConnection(t *testing.T) {
	service := New(Config{ReuseExistingCDP: true}, nil)
	service.discoverCDP = func(context.Context) ([]cdpCandidate, error) { return nil, nil }
	url, mode, err := service.resolveCDPConnection(context.Background(), StartRequest{})
	if err != nil || url != "" || mode != "owned" {
		t.Fatalf("zero candidates = url %q mode %q err %v", url, mode, err)
	}

	service.discoverCDP = func(context.Context) ([]cdpCandidate, error) {
		return []cdpCandidate{{URL: "http://127.0.0.1:9222", Source: "process"}}, nil
	}
	url, mode, err = service.resolveCDPConnection(context.Background(), StartRequest{})
	if err != nil || url != "http://127.0.0.1:9222" || mode != "external_discovered" {
		t.Fatalf("one candidate = url %q mode %q err %v", url, mode, err)
	}

	service.discoverCDP = func(context.Context) ([]cdpCandidate, error) {
		return []cdpCandidate{{URL: "http://127.0.0.1:9222"}, {URL: "http://127.0.0.1:9223"}}, nil
	}
	_, _, err = service.resolveCDPConnection(context.Background(), StartRequest{})
	var browserErr *Error
	if !errors.As(err, &browserErr) || browserErr.Code != ErrCDPAmbiguous || browserErr.Details == nil || browserErr.Details.Count != 2 {
		t.Fatalf("multiple candidates error = %#v", err)
	}
}
