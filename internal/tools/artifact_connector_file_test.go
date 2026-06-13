package tools

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/artifactrelay"
	"github.com/uvwt/agentdock/internal/workspace"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestResolveArtifactSendSourceUsesConnectorMountedPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "mounted.txt")
	if err := os.WriteFile(path, []byte("mounted payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	resolved, cleanup, err := resolveArtifactSendSource(
		context.Background(),
		ws,
		map[string]any{"local_path": "mounted.txt", "download_url": "https://files.example.test/fallback"},
		"",
		filepath.Join(root, "connector-input"),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if resolved != path {
		t.Fatalf("resolved = %q, want %q", resolved, path)
	}
}

func TestResolveArtifactSendSourceDownloadsConnectorReference(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(root, false)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", request.Method)
		}
		if request.URL.Host != "files.example.test" {
			t.Fatalf("host = %s", request.URL.Host)
		}
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(strings.NewReader("connector payload")),
			ContentLength: int64(len("connector payload")),
			Header:        make(http.Header),
			Request:       request,
		}, nil
	})}
	tempRoot := filepath.Join(root, "connector-input")
	resolved, cleanup, err := resolveArtifactSendSource(
		context.Background(),
		ws,
		map[string]any{
			"local_path":   "/missing/proxied/mount.txt",
			"download_url": "https://files.example.test/object",
			"filename":     "report.txt",
		},
		"",
		tempRoot,
		client,
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "connector payload" {
		t.Fatalf("payload = %q", data)
	}
	if filepath.Base(resolved) != "report.txt" {
		t.Fatalf("filename = %q", filepath.Base(resolved))
	}
	info, err := os.Stat(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o", info.Mode().Perm())
	}
	parentInfo, err := os.Stat(filepath.Dir(resolved))
	if err != nil {
		t.Fatal(err)
	}
	if parentInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %o", parentInfo.Mode().Perm())
	}
	cleanup()
	if _, err := os.Stat(resolved); !os.IsNotExist(err) {
		t.Fatalf("temporary plaintext still exists: %v", err)
	}
}

func TestConnectorFileReferenceRejectsUnsafeAddressesAndOversize(t *testing.T) {
	for _, rawURL := range []string{
		"http://files.example.test/object",
		"https://127.0.0.1/object",
		"https://localhost/object",
	} {
		if err := validateConnectorDownloadURL(mustParseURL(t, rawURL)); err == nil {
			t.Fatalf("accepted unsafe URL %s", rawURL)
		}
	}

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Body:          io.NopCloser(strings.NewReader("x")),
			ContentLength: artifactrelay.MaxCipherBytes + 1,
			Header:        make(http.Header),
			Request:       request,
		}, nil
	})}
	if _, _, err := downloadConnectorFile(context.Background(), client, "https://files.example.test/oversize", "large.bin", t.TempDir()); err == nil {
		t.Fatal("oversize connector file was accepted")
	}
}

func mustParseURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
