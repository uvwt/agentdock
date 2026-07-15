package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRunDownloadsVerifiesAndAppliesRelease(t *testing.T) {
	archive := makeTarGz(t, "bin/agentdock", []byte("new-binary"))
	digest := sha256.Sum256(archive)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			_ = json.NewEncoder(w).Encode(release{
				TagName: "v0.4.5",
				Assets: []releaseAsset{
					{Name: "agentdock_darwin_arm64.tar.gz", URL: server.URL + "/archive"},
					{Name: "agentdock_darwin_arm64.tar.gz.sha256", URL: server.URL + "/checksum"},
				},
			})
		case "/archive":
			_, _ = w.Write(archive)
		case "/checksum":
			fmt.Fprintf(w, "%s  agentdock_darwin_arm64.tar.gz\n", hex.EncodeToString(digest[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var output strings.Builder
	applied := false
	err := run(context.Background(), options{
		CurrentVersion: "0.4.4",
		ExecutablePath: "/tmp/agentdock",
		GOOS:           "darwin",
		GOARCH:         "arm64",
		ReleaseAPI:     server.URL + "/release",
		HTTPClient:     server.Client(),
		Output:         &output,
		VerifyBinary: func(_ context.Context, path, targetVersion string) error {
			file := mustOpen(t, path)
			data, err := io.ReadAll(file)
			if err != nil {
				return err
			}
			if string(data) != "new-binary" || targetVersion != "v0.4.5" {
				return fmt.Errorf("unexpected staged binary or version")
			}
			return nil
		},
		Apply: func(_ context.Context, request applyRequest) (applyResult, error) {
			applied = true
			if request.CurrentPath != "/tmp/agentdock" || request.TargetVersion != "v0.4.5" {
				t.Fatalf("unexpected apply request: %#v", request)
			}
			return applyResult{Restarted: true}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("release was not applied")
	}
	if !strings.Contains(output.String(), "更新完成并已重启：v0.4.4 → v0.4.5") {
		t.Fatalf("unexpected output: %s", output.String())
	}
}

func TestRunRejectsChecksumBeforeApplying(t *testing.T) {
	archive := makeTarGz(t, "bin/agentdock", []byte("new-binary"))
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			_ = json.NewEncoder(w).Encode(release{
				TagName: "v0.4.5",
				Assets: []releaseAsset{
					{Name: "agentdock_linux_amd64.tar.gz", URL: server.URL + "/archive"},
					{Name: "agentdock_linux_amd64.tar.gz.sha256", URL: server.URL + "/checksum"},
				},
			})
		case "/archive":
			_, _ = w.Write(archive)
		case "/checksum":
			fmt.Fprintln(w, strings.Repeat("0", 64), " agentdock_linux_amd64.tar.gz")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	err := run(context.Background(), options{
		CurrentVersion: "0.4.4",
		ExecutablePath: "/tmp/agentdock",
		GOOS:           "linux",
		GOARCH:         "amd64",
		ReleaseAPI:     server.URL + "/release",
		HTTPClient:     server.Client(),
		VerifyBinary: func(context.Context, string, string) error {
			t.Fatal("binary verification must not run after checksum failure")
			return nil
		},
		Apply: func(context.Context, applyRequest) (applyResult, error) {
			t.Fatal("apply must not run after checksum failure")
			return applyResult{}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "SHA-256 不匹配") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSkipsCurrentAndNewerVersions(t *testing.T) {
	for _, current := range []string{"0.4.5", "0.4.6"} {
		t.Run(current, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(release{TagName: "v0.4.5"})
			}))
			defer server.Close()
			var output strings.Builder
			err := run(context.Background(), options{
				CurrentVersion: current,
				ExecutablePath: "/tmp/agentdock",
				GOOS:           "darwin",
				GOARCH:         "arm64",
				ReleaseAPI:     server.URL,
				HTTPClient:     server.Client(),
				Output:         &output,
				VerifyBinary:   func(context.Context, string, string) error { return nil },
				Apply: func(context.Context, applyRequest) (applyResult, error) {
					t.Fatal("apply must not run")
					return applyResult{}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestParseVersionOutputRequiresExactVersionLine(t *testing.T) {
	version, err := parseVersionOutput([]byte("AgentDock v0.4.5\ncommit: abc\n"))
	if err != nil || version != "v0.4.5" {
		t.Fatalf("version=%q err=%v", version, err)
	}
	for _, output := range []string{
		"AgentDock 0.4.5\n",
		"AgentDock v0.4.50\n",
		"AgentDock v0.4\n",
	} {
		parsed, parseErr := parseVersionOutput([]byte(output))
		if output == "AgentDock v0.4.50\n" {
			if parseErr != nil || parsed != "v0.4.50" {
				t.Fatalf("valid exact version rejected: parsed=%q err=%v", parsed, parseErr)
			}
			continue
		}
		if parseErr == nil {
			t.Fatalf("invalid output accepted: %q -> %q", output, parsed)
		}
	}
}

func TestPlatformAssetNames(t *testing.T) {
	tests := []struct {
		goos, goarch string
		archive      string
		executable   string
	}{
		{goos: "darwin", goarch: "arm64", archive: "agentdock_darwin_arm64.tar.gz", executable: "agentdock"},
		{goos: "linux", goarch: "amd64", archive: "agentdock_linux_amd64.tar.gz", executable: "agentdock"},
		{goos: "windows", goarch: "arm64", archive: "agentdock_windows_arm64.zip", executable: "agentdock.exe"},
	}
	for _, test := range tests {
		archive, executable, err := platformAssetNames(test.goos, test.goarch)
		if err != nil {
			t.Fatal(err)
		}
		if archive != test.archive || executable != test.executable {
			t.Fatalf("%s/%s = %s %s", test.goos, test.goarch, archive, executable)
		}
	}
}

func makeTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}
