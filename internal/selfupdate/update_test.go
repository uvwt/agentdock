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
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseArchiveLimitSupportsCurrentWindowsBundle(t *testing.T) {
	// Windows 离线包包含自包含 WPF 控制面板，压缩后已超过 64 MiB；
	// 下载上限需要留有增长空间，但单个解压内容仍保持原来的 64 MiB 防护。
	const minimumArchiveLimit = 96 << 20
	if maxReleaseArchiveBytes < minimumArchiveLimit {
		t.Fatalf("release archive limit too small: got %d, want at least %d", maxReleaseArchiveBytes, minimumArchiveLimit)
	}
	if maxExtractedPayloadBytes != 64<<20 {
		t.Fatalf("extracted payload limit changed unexpectedly: %d", maxExtractedPayloadBytes)
	}
}

func TestInspectUpdateReportsAvailableVersionWithoutApplying(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(release{
			TagName: "v0.6.2",
			Assets: []releaseAsset{
				{Name: "agentdock_windows_amd64.zip", URL: "https://example.invalid/archive"},
				{Name: "agentdock_windows_amd64.zip.sha256", URL: "https://example.invalid/checksum"},
			},
		})
	}))
	defer server.Close()

	inspection, err := inspectUpdate(context.Background(), options{
		CurrentVersion: "0.6.1",
		GOOS:           "windows",
		GOARCH:         "amd64",
		ReleaseAPI:     server.URL,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.Result.UpdateAvailable || inspection.Result.CurrentVersion != "v0.6.1" || inspection.Result.LatestVersion != "v0.6.2" {
		t.Fatalf("unexpected check result: %#v", inspection.Result)
	}
	if inspection.ArchiveName != "agentdock_windows_amd64.zip" || inspection.ExecutableName != "agentdock.exe" {
		t.Fatalf("unexpected inspection assets: %#v", inspection)
	}
}

func TestInspectUpdateRequiresMacOSDesktopAssetsWhenAppIsInstalled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(release{
			TagName: "v0.7.1",
			Assets: []releaseAsset{
				{Name: "agentdock_darwin_arm64.tar.gz", URL: "https://example.invalid/core"},
				{Name: "agentdock_darwin_arm64.tar.gz.sha256", URL: "https://example.invalid/core-checksum"},
				{Name: macOSDesktopArchiveName, URL: "https://example.invalid/app"},
				{Name: macOSDesktopArchiveName + ".sha256", URL: "https://example.invalid/app-checksum"},
			},
		})
	}))
	defer server.Close()

	inspection, err := inspectUpdate(context.Background(), options{
		CurrentVersion:    "0.7.0",
		DesktopTargetPath: "/Applications/AgentDock.app",
		GOOS:              "darwin",
		GOARCH:            "arm64",
		ReleaseAPI:        server.URL,
		HTTPClient:        server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.DesktopArchiveAsset.Name != macOSDesktopArchiveName ||
		inspection.DesktopChecksumAsset.Name != macOSDesktopArchiveName+".sha256" {
		t.Fatalf("unexpected desktop assets: %#v", inspection)
	}
}

func TestInspectUpdateRepairsOlderDesktopWhenCoreIsCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(release{
			TagName: "v0.7.1",
			Assets: []releaseAsset{
				{Name: "agentdock_darwin_arm64.tar.gz", URL: "https://example.invalid/core"},
				{Name: "agentdock_darwin_arm64.tar.gz.sha256", URL: "https://example.invalid/core-checksum"},
				{Name: macOSDesktopArchiveName, URL: "https://example.invalid/app"},
				{Name: macOSDesktopArchiveName + ".sha256", URL: "https://example.invalid/app-checksum"},
			},
		})
	}))
	defer server.Close()

	inspection, err := inspectUpdate(context.Background(), options{
		CurrentVersion:        "0.7.1",
		DesktopTargetPath:     "/Applications/AgentDock.app",
		DesktopCurrentVersion: "0.6.1",
		GOOS:                  "darwin",
		GOARCH:                "arm64",
		ReleaseAPI:            server.URL,
		HTTPClient:            server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.Result.UpdateAvailable || !inspection.Result.DesktopUpdateAvailable {
		t.Fatalf("desktop repair was not reported: %#v", inspection.Result)
	}
	if inspection.Result.Message != "发现控制面板更新：v0.6.1 → v0.7.1" {
		t.Fatalf("unexpected desktop repair message: %s", inspection.Result.Message)
	}
}

func TestInspectUpdateUsesWindowsReleaseBundleForDesktopUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(release{
			TagName: "v0.7.5",
			Assets: []releaseAsset{
				{Name: "agentdock_windows_amd64.zip", URL: "https://example.invalid/windows"},
				{Name: "agentdock_windows_amd64.zip.sha256", URL: "https://example.invalid/windows.sha256"},
			},
		})
	}))
	defer server.Close()

	inspection, err := inspectUpdate(context.Background(), options{
		CurrentVersion:        "0.7.4",
		DesktopTargetPath:     `C:\Users\test\AppData\Local\AgentDock`,
		DesktopCurrentVersion: "0.7.4",
		GOOS:                  "windows",
		GOARCH:                "amd64",
		ReleaseAPI:            server.URL,
		HTTPClient:            server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.Result.UpdateAvailable || !inspection.Result.DesktopUpdateAvailable {
		t.Fatalf("Windows desktop update was not reported: %#v", inspection.Result)
	}
	if inspection.DesktopArchiveAsset != inspection.ArchiveAsset || inspection.DesktopChecksumAsset != inspection.ChecksumAsset {
		t.Fatalf("Windows desktop update must reuse the platform archive: %#v", inspection)
	}
}

func TestInspectUpdateRepairsMissingWindowsDesktopMarkerWhenCoreIsCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(release{
			TagName: "v0.7.5",
			Assets: []releaseAsset{
				{Name: "agentdock_windows_amd64.zip", URL: "https://example.invalid/windows"},
				{Name: "agentdock_windows_amd64.zip.sha256", URL: "https://example.invalid/windows.sha256"},
			},
		})
	}))
	defer server.Close()

	inspection, err := inspectUpdate(context.Background(), options{
		CurrentVersion:    "0.7.5",
		DesktopTargetPath: `C:\Users\test\AppData\Local\AgentDock`,
		GOOS:              "windows",
		GOARCH:            "amd64",
		ReleaseAPI:        server.URL,
		HTTPClient:        server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.Result.UpdateAvailable || !inspection.Result.DesktopUpdateAvailable {
		t.Fatalf("missing Windows desktop marker was not repairable: %#v", inspection.Result)
	}
	if inspection.DesktopArchiveAsset != inspection.ArchiveAsset {
		t.Fatalf("Windows desktop repair did not reuse the release archive: %#v", inspection)
	}
}

func TestInspectUpdateRejectsMacOSReleaseWithoutDesktopAsset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(release{
			TagName: "v0.7.1",
			Assets: []releaseAsset{
				{Name: "agentdock_darwin_arm64.tar.gz", URL: "https://example.invalid/core"},
				{Name: "agentdock_darwin_arm64.tar.gz.sha256", URL: "https://example.invalid/core-checksum"},
			},
		})
	}))
	defer server.Close()

	_, err := inspectUpdate(context.Background(), options{
		CurrentVersion:    "0.7.0",
		DesktopTargetPath: "/Applications/AgentDock.app",
		GOOS:              "darwin",
		GOARCH:            "arm64",
		ReleaseAPI:        server.URL,
		HTTPClient:        server.Client(),
	})
	if err == nil || !strings.Contains(err.Error(), macOSDesktopArchiveName) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInspectUpdateReportsCurrentVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(release{TagName: "v0.6.1"})
	}))
	defer server.Close()

	inspection, err := inspectUpdate(context.Background(), options{
		CurrentVersion: "0.6.1",
		GOOS:           "windows",
		GOARCH:         "amd64",
		ReleaseAPI:     server.URL,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Result.UpdateAvailable || inspection.Result.Message != "当前已是最新版本：v0.6.1" {
		t.Fatalf("unexpected current-version result: %#v", inspection.Result)
	}
}

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
			if _, err := os.Stat(filepath.Join(request.BundlePath, "manifest.json")); err != nil {
				t.Fatalf("core Skill Bundle was not extracted: %v", err)
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

func TestRunStagesMacOSDesktopAppWithCoreUpdate(t *testing.T) {
	coreArchive := makeTarGz(t, "bin/agentdock", []byte("new-core"))
	coreDigest := sha256.Sum256(coreArchive)
	desktopArchive := []byte("signed-app-archive")
	desktopDigest := sha256.Sum256(desktopArchive)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			_ = json.NewEncoder(w).Encode(release{
				TagName: "v0.7.1",
				Assets: []releaseAsset{
					{Name: "agentdock_darwin_arm64.tar.gz", URL: server.URL + "/core"},
					{Name: "agentdock_darwin_arm64.tar.gz.sha256", URL: server.URL + "/core.sha256"},
					{Name: macOSDesktopArchiveName, URL: server.URL + "/desktop"},
					{Name: macOSDesktopArchiveName + ".sha256", URL: server.URL + "/desktop.sha256"},
				},
			})
		case "/core":
			_, _ = w.Write(coreArchive)
		case "/core.sha256":
			fmt.Fprintf(w, "%s  agentdock_darwin_arm64.tar.gz\n", hex.EncodeToString(coreDigest[:]))
		case "/desktop":
			_, _ = w.Write(desktopArchive)
		case "/desktop.sha256":
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(desktopDigest[:]), macOSDesktopArchiveName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	extracted := false
	applied := false
	err := run(context.Background(), options{
		CurrentVersion:    "0.7.0",
		ExecutablePath:    "/tmp/agentdock",
		DesktopTargetPath: "/Applications/AgentDock.app",
		GOOS:              "darwin",
		GOARCH:            "arm64",
		ReleaseAPI:        server.URL + "/release",
		HTTPClient:        server.Client(),
		Output:            io.Discard,
		VerifyBinary:      func(context.Context, string, string) error { return nil },
		ExtractDesktop: func(_ context.Context, data []byte, tempDir, targetVersion string) (string, error) {
			extracted = true
			if string(data) != string(desktopArchive) || targetVersion != "v0.7.1" {
				return "", fmt.Errorf("unexpected desktop payload")
			}
			path := filepath.Join(tempDir, "AgentDock.app")
			if err := os.Mkdir(path, 0o700); err != nil {
				return "", err
			}
			return path, nil
		},
		Apply: func(_ context.Context, request applyRequest) (applyResult, error) {
			applied = true
			if request.DesktopTargetPath != "/Applications/AgentDock.app" ||
				filepath.Base(request.DesktopStagedPath) != "AgentDock.app" {
				t.Fatalf("unexpected desktop apply request: %#v", request)
			}
			return applyResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !extracted || !applied {
		t.Fatalf("desktop update flow incomplete: extracted=%v applied=%v", extracted, applied)
	}
}

func TestRunDesktopOnlyDoesNotRequireCoreAsset(t *testing.T) {
	desktopArchive := []byte("signed-app-archive")
	desktopDigest := sha256.Sum256(desktopArchive)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			_ = json.NewEncoder(w).Encode(release{
				TagName: "v0.7.1",
				Assets: []releaseAsset{
					{Name: macOSDesktopArchiveName, URL: server.URL + "/desktop"},
					{Name: macOSDesktopArchiveName + ".sha256", URL: server.URL + "/desktop.sha256"},
				},
			})
		case "/desktop":
			_, _ = w.Write(desktopArchive)
		case "/desktop.sha256":
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(desktopDigest[:]), macOSDesktopArchiveName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	applied := false
	err := run(context.Background(), options{
		CurrentVersion:        "0.7.0",
		ExecutablePath:        "/Applications/AgentDock.app/Contents/Helpers/agentdock",
		DesktopTargetPath:     "/Applications/AgentDock.app",
		DesktopCurrentVersion: "0.7.0",
		DesktopOnly:           true,
		GOOS:                  "darwin",
		GOARCH:                "arm64",
		ReleaseAPI:            server.URL + "/release",
		HTTPClient:            server.Client(),
		Output:                io.Discard,
		VerifyBinary:          func(context.Context, string, string) error { return nil },
		ExtractDesktop: func(_ context.Context, data []byte, tempDir, targetVersion string) (string, error) {
			if string(data) != string(desktopArchive) || targetVersion != "v0.7.1" {
				return "", fmt.Errorf("unexpected desktop payload")
			}
			path := filepath.Join(tempDir, "AgentDock.app")
			if err := os.Mkdir(path, 0o700); err != nil {
				return "", err
			}
			return path, nil
		},
		Apply: func(_ context.Context, request applyRequest) (applyResult, error) {
			applied = true
			if !request.DesktopOnly || request.StagedPath != "" || request.BundlePath != "" {
				t.Fatalf("desktop-only update unexpectedly staged standalone core: %#v", request)
			}
			return applyResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("desktop-only update was not applied")
	}
}

func TestRunRepairsWindowsDesktopOnlyWhenCoreIsCurrent(t *testing.T) {
	desktopArchive := []byte("windows-release-bundle")
	desktopDigest := sha256.Sum256(desktopArchive)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			_ = json.NewEncoder(w).Encode(release{
				TagName: "v0.7.5",
				Assets: []releaseAsset{
					{Name: "agentdock_windows_amd64.zip", URL: server.URL + "/windows"},
					{Name: "agentdock_windows_amd64.zip.sha256", URL: server.URL + "/windows.sha256"},
				},
			})
		case "/windows":
			_, _ = w.Write(desktopArchive)
		case "/windows.sha256":
			fmt.Fprintf(w, "%s  agentdock_windows_amd64.zip\n", hex.EncodeToString(desktopDigest[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	extracted := false
	applied := false
	err := run(context.Background(), options{
		CurrentVersion:        "0.7.5",
		ExecutablePath:        `C:\Users\test\AppData\Local\AgentDock\bin\agentdock.exe`,
		DesktopTargetPath:     `C:\Users\test\AppData\Local\AgentDock`,
		DesktopCurrentVersion: "0.7.4",
		GOOS:                  "windows",
		GOARCH:                "amd64",
		ReleaseAPI:            server.URL + "/release",
		HTTPClient:            server.Client(),
		Output:                io.Discard,
		VerifyBinary:          func(context.Context, string, string) error { return nil },
		ExtractDesktop: func(_ context.Context, data []byte, tempDir, targetVersion string) (string, error) {
			extracted = true
			if string(data) != string(desktopArchive) || targetVersion != "v0.7.5" {
				return "", fmt.Errorf("unexpected Windows desktop payload")
			}
			path := filepath.Join(tempDir, "windows-desktop")
			if err := os.Mkdir(path, 0o700); err != nil {
				return "", err
			}
			return path, nil
		},
		Apply: func(_ context.Context, request applyRequest) (applyResult, error) {
			applied = true
			if !request.DesktopOnly || request.StagedPath != "" || request.BundlePath != "" || request.TargetVersion != "v0.7.5" {
				t.Fatalf("unexpected Windows desktop-only request: %#v", request)
			}
			return applyResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !extracted || !applied {
		t.Fatalf("Windows desktop repair flow incomplete: extracted=%v applied=%v", extracted, applied)
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
	entries := []struct {
		name    string
		content []byte
		mode    int64
	}{
		{name: name, content: content, mode: 0o755},
		{name: coreSkillBundlePrefix + "manifest.json", content: []byte(`{"skills":[]}`), mode: 0o600},
	}
	for _, entry := range entries {
		if err := tarWriter.WriteHeader(&tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(entry.content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(entry.content); err != nil {
			t.Fatal(err)
		}
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

func TestDownloadWithProgressReportsKnownLength(t *testing.T) {
	payload := bytes.Repeat([]byte("agentdock"), 32*1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	var reports [][2]int64
	data, err := downloadWithProgress(context.Background(), server.Client(), server.URL, int64(len(payload))+1, func(read, total int64) {
		reports = append(reports, [2]int64{read, total})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatal("downloaded payload mismatch")
	}
	if len(reports) < 2 {
		t.Fatalf("expected initial and final progress reports, got %v", reports)
	}
	last := reports[len(reports)-1]
	if last[0] != int64(len(payload)) || last[1] != int64(len(payload)) {
		t.Fatalf("unexpected final progress: %v", last)
	}
}

func TestDownloadWithProgressSupportsUnknownLength(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 96*1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	var lastRead, lastTotal int64
	data, err := downloadWithProgress(context.Background(), server.Client(), server.URL, int64(len(payload))+1, func(read, total int64) {
		lastRead, lastTotal = read, total
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatal("downloaded payload mismatch")
	}
	if lastRead != int64(len(payload)) || lastTotal != -1 {
		t.Fatalf("unexpected unknown-length progress: read=%d total=%d", lastRead, lastTotal)
	}
}
