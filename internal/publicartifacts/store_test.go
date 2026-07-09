package publicartifacts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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
