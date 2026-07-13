package publicartifacts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestPublishFileCreatesImmutableSignedSnapshot(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "report.txt")
	if err := os.WriteFile(source, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := New(filepath.Join(root, "home"), "https://agent.example", 8765)
	result, err := store.Publish(PublishRequest{Path: source, RetentionSeconds: 60, Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if result.URL == "" || !strings.HasPrefix(result.URL, "https://agent.example/artifacts/public/") {
		t.Fatalf("url = %q", result.URL)
	}
	if result.Filename != "report.txt" || result.Size != int64(len("first")) || result.SHA256 == "" || result.Archive {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
	if err := os.WriteFile(source, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	body, status := download(t, store, result.URL)
	if status != http.StatusOK {
		t.Fatalf("status = %d body=%q", status, body)
	}
	if string(body) != "first" {
		t.Fatalf("snapshot body = %q", body)
	}
}

func TestPublishWithoutBaseURLReturnsArtifactReferenceAndCanRead(t *testing.T) {
	root := t.TempDir()
	data := []byte("artifact-only")
	store := New(filepath.Join(root, "home"), "", 8765)

	result, err := store.PublishBytes(PublishBytesRequest{Filename: "artifact.bin", Data: data, MimeType: "application/octet-stream", RetentionSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if result.ArtifactID == "" || result.URL != "" {
		t.Fatalf("artifact-only result = %#v", result)
	}
	if _, err := os.Stat(store.SecretPath); !os.IsNotExist(err) {
		t.Fatalf("publishing without a public URL should not create a signing secret: %v", err)
	}

	meta, readData, err := store.Read(result.ArtifactID, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if meta.ArtifactID != result.ArtifactID || !bytes.Equal(readData, data) {
		t.Fatalf("read artifact mismatch: meta=%#v data=%q", meta, readData)
	}

	payload := filepath.Join(store.Root, result.ArtifactID, "payload")
	if err := os.WriteFile(payload, []byte("tampered-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Read(result.ArtifactID, 1024); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered artifact should fail checksum verification: %v", err)
	}
}

func TestPublishDirectoryCreatesTarGzSnapshot(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "bundle")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "file.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := New(filepath.Join(root, "home"), "https://agent.example", 8765)
	result, err := store.Publish(PublishRequest{Path: dir, RetentionSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Archive || result.Filename != "bundle.tar.gz" || result.MimeType != "application/gzip" {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
	body, status := download(t, store, result.URL)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	found := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == "bundle/nested/file.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("archive did not include nested file")
	}
}

func TestPublishBytesImageUsesInlineDispositionAndDimensions(t *testing.T) {
	root := t.TempDir()
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADUlEQVR4nGJgYGD4DwABBAEAgh7R8QAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	store := New(filepath.Join(root, "home"), "https://agent.example", 8765)
	result, err := store.PublishBytes(PublishBytesRequest{Filename: "tiny.png", Data: data, MimeType: "image/png", RetentionSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if result.Size != int64(len(data)) || result.Width != 1 || result.Height != 1 {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
	metadataBytes, err := os.ReadFile(filepath.Join(store.Root, result.ArtifactID, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	metadataText := string(metadataBytes)
	if !strings.Contains(metadataText, `"size_bytes"`) || strings.Contains(metadataText, `"size"`) {
		t.Fatalf("metadata json should use size_bytes only: %s", metadataText)
	}
	req := httptest.NewRequest(http.MethodGet, result.URL, nil)
	recorder := httptest.NewRecorder()
	store.ServeHTTP(recorder, req, "/artifacts/public/")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q", recorder.Code, recorder.Body.String())
	}
	if disposition := recorder.Header().Get("Content-Disposition"); !strings.HasPrefix(disposition, "inline") {
		t.Fatalf("Content-Disposition = %q, want inline", disposition)
	}
}

func TestSafeDownloadNamePreservesValidUTF8WithinByteLimit(t *testing.T) {
	name := strings.Repeat("文", 100) + ".txt"
	got := safeDownloadName(name)
	if !utf8.ValidString(got) {
		t.Fatalf("safeDownloadName() returned invalid UTF-8: %q", got)
	}
	if len(got) > 240 {
		t.Fatalf("safeDownloadName() bytes = %d, want <= 240", len(got))
	}
	if !strings.HasSuffix(got, ".txt") {
		t.Fatalf("safeDownloadName() = %q, want .txt suffix", got)
	}
	if got == name {
		t.Fatal("safeDownloadName() did not truncate an oversized name")
	}

	invalid := safeDownloadName("report-\xff.txt")
	if !utf8.ValidString(invalid) || invalid != "report-_.txt" {
		t.Fatalf("safeDownloadName() invalid input = %q", invalid)
	}
	if got := safeDownloadName("\\"); got != "artifact.bin" {
		t.Fatalf("safeDownloadName(backslash) = %q", got)
	}
	if got := safeDownloadName("line\nfeed.txt"); got != "line_feed.txt" {
		t.Fatalf("safeDownloadName(control) = %q", got)
	}
}

func FuzzSafeDownloadName(f *testing.F) {
	for _, seed := range []string{"", "\\", "report.txt", strings.Repeat("文", 100) + ".txt", "../secret", "report-\xff.txt"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		got := safeDownloadName(input)
		if got == "" || !utf8.ValidString(got) {
			t.Fatalf("safeDownloadName(%q) = %q", input, got)
		}
		if len(got) > 240 {
			t.Fatalf("safeDownloadName(%q) bytes = %d", input, len(got))
		}
		if strings.ContainsAny(got, "/\\") || got == "." || got == ".." {
			t.Fatalf("safeDownloadName(%q) returned unsafe basename %q", input, got)
		}
		if strings.IndexFunc(got, func(char rune) bool { return char < 0x20 || char == 0x7f }) >= 0 {
			t.Fatalf("safeDownloadName(%q) returned control character in %q", input, got)
		}
	})
}

func TestPublishCapsRetentionAtSevenDays(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "a.txt")
	if err := os.WriteFile(source, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0).UTC()
	store := New(filepath.Join(root, "home"), "https://agent.example", 8765)
	result, err := store.Publish(PublishRequest{Path: source, RetentionSeconds: int((MaxRetention + time.Hour) / time.Second), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.ExpiresAt.Sub(now); got != MaxRetention {
		t.Fatalf("retention = %s", got)
	}
}

func TestDownloadRejectsBadSignatureExpiredAndFilenameMismatch(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "a.txt")
	if err := os.WriteFile(source, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := New(filepath.Join(root, "home"), "https://agent.example", 8765)
	result, err := store.Publish(PublishRequest{Path: source, RetentionSeconds: 60, Now: time.Now().UTC().Add(-2 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, status := download(t, store, result.URL); status != http.StatusGone {
		t.Fatalf("expired status = %d", status)
	}

	fresh, err := store.Publish(PublishRequest{Path: source, RetentionSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(fresh.URL)
	q := u.Query()
	q.Set("sig", "bad")
	u.RawQuery = q.Encode()
	if _, status := download(t, store, u.String()); status != http.StatusNotFound {
		t.Fatalf("bad sig status = %d", status)
	}
	u, _ = url.Parse(fresh.URL)
	u.Path = strings.Replace(u.Path, "/a.txt", "/b.txt", 1)
	if _, status := download(t, store, u.String()); status != http.StatusNotFound {
		t.Fatalf("name mismatch status = %d", status)
	}
	u.Path = "/artifacts/public/" + fresh.ArtifactID + "/..%2Fsecret.txt"
	if _, status := download(t, store, u.String()); status != http.StatusNotFound {
		t.Fatalf("traversal status = %d", status)
	}
}

func TestSecretCreatedWithPrivatePermissionsAndReused(t *testing.T) {
	root := t.TempDir()
	store := New(filepath.Join(root, "home"), "", 1234)
	if err := store.EnsureSecret(); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(store.SecretPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.SecretPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode = %o", info.Mode().Perm())
	}
	if err := store.EnsureSecret(); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(store.SecretPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("secret was regenerated")
	}
}

func TestSecretInitializationIsAtomicUnderConcurrency(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "home"), "", 1234)
	const workers = 64
	secrets := make(chan string, workers)
	errs := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			<-start
			secret, err := store.ensureSecret()
			if err != nil {
				errs <- err
				return
			}
			secrets <- string(secret)
		}()
	}
	close(start)
	wg.Wait()
	close(secrets)
	close(errs)
	for err := range errs {
		t.Fatalf("ensureSecret() error = %v", err)
	}
	var expected string
	for secret := range secrets {
		if expected == "" {
			expected = secret
		}
		if secret != expected {
			t.Fatal("concurrent initialization returned different secrets")
		}
	}
	if len(expected) != 32 {
		t.Fatalf("secret length = %d, want 32", len(expected))
	}
	entries, err := os.ReadDir(filepath.Dir(store.SecretPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(store.SecretPath) {
		t.Fatalf("secret directory contains temporary artifacts: %#v", entries)
	}
}

func TestConcurrentPublishBytesCreatesDistinctArtifacts(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "home"), "https://agentdock.example", 1234)
	const workers = 32
	results := make(chan PublishResult, workers)
	errs := make(chan error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func(index int) {
			defer wg.Done()
			<-start
			result, err := store.PublishBytes(PublishBytesRequest{
				Filename: "artifact.txt", Data: []byte(fmt.Sprintf("payload-%d", index)),
				MimeType: "text/plain", RetentionSeconds: 60,
			})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("PublishBytes() error = %v", err)
	}
	seen := make(map[string]struct{}, workers)
	for result := range results {
		if _, exists := seen[result.Metadata.ArtifactID]; exists {
			t.Fatalf("duplicate artifact id = %q", result.Metadata.ArtifactID)
		}
		seen[result.Metadata.ArtifactID] = struct{}{}
		if !strings.Contains(result.URL, result.Metadata.ArtifactID) {
			t.Fatalf("URL %q does not contain artifact id %q", result.URL, result.Metadata.ArtifactID)
		}
	}
	if len(seen) != workers {
		t.Fatalf("published artifacts = %d, want %d", len(seen), workers)
	}
}

func TestInvalidExistingSecretIsNotSilentlyReplaced(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "home"), "", 1234)
	if err := os.MkdirAll(filepath.Dir(store.SecretPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.SecretPath, []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ensureSecret(); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("ensureSecret() error = %v, want invalid-secret error", err)
	}
	data, err := os.ReadFile(store.SecretPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "invalid\n" {
		t.Fatalf("invalid secret was overwritten: %q", data)
	}
}

func TestCleanupRemovesExpiredAndOldBrokenArtifacts(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "a.txt")
	if err := os.WriteFile(source, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := New(filepath.Join(root, "home"), "", 8765)
	expired, err := store.Publish(PublishRequest{Path: source, RetentionSeconds: 1, Now: time.Unix(1000, 0)})
	if err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(store.Root, "broken")
	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "metadata.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-25 * time.Hour)
	_ = os.Chtimes(broken, old, old)
	if err := store.Cleanup(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Root, expired.ArtifactID)); !os.IsNotExist(err) {
		t.Fatalf("expired artifact still exists: %v", err)
	}
	if _, err := os.Stat(broken); !os.IsNotExist(err) {
		t.Fatalf("broken artifact still exists: %v", err)
	}
}

func TestCleanupReportsRemovalFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission semantics differ on Windows")
	}
	store := New(filepath.Join(t.TempDir(), "home"), "", 8765)
	artifactDir := filepath.Join(store.Root, "expired")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	meta := Metadata{
		ArtifactID: "expired", Filename: "payload.txt", SHA256: strings.Repeat("0", 64),
		Size: 1, CreatedAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour),
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "metadata.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "payload"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.Root, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(store.Root, 0o700)
	if err := store.Cleanup(time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "remove artifact directory") {
		t.Fatalf("Cleanup() error = %v, want removal failure", err)
	}
	if _, err := os.Stat(artifactDir); err != nil {
		t.Fatalf("artifact should remain after failed cleanup: %v", err)
	}
}

func download(t *testing.T, store Store, rawURL string) ([]byte, int) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, u.String(), nil)
	recorder := httptest.NewRecorder()
	store.ServeHTTP(recorder, req, "/artifacts/public/")
	return recorder.Body.Bytes(), recorder.Code
}

func TestActiveContentArtifactsAreAttachmentsWithSandboxPolicy(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "home"), "https://agent.example", 8765)
	for _, test := range []struct {
		name     string
		filename string
		mimeType string
		data     string
	}{
		{name: "html", filename: "page.html", mimeType: "text/html", data: "<script>alert(1)</script>"},
		{name: "svg", filename: "image.svg", mimeType: "image/svg+xml", data: `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`},
		{name: "xml", filename: "data.xml", mimeType: "application/xml", data: "<root/>"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := store.PublishBytes(PublishBytesRequest{Filename: test.filename, Data: []byte(test.data), MimeType: test.mimeType, RetentionSeconds: 60})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, result.URL, nil)
			recorder := httptest.NewRecorder()
			store.ServeHTTP(recorder, req, "/artifacts/public/")
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d body=%q", recorder.Code, recorder.Body.String())
			}
			if disposition := recorder.Header().Get("Content-Disposition"); !strings.HasPrefix(disposition, "attachment") {
				t.Fatalf("Content-Disposition = %q, want attachment", disposition)
			}
			policy := recorder.Header().Get("Content-Security-Policy")
			if !strings.Contains(policy, "sandbox") || !strings.Contains(policy, "default-src 'none'") {
				t.Fatalf("Content-Security-Policy = %q", policy)
			}
			if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing nosniff response header")
			}
		})
	}
}
