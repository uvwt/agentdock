package component

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultCatalogUsesNexusDockDistribution(t *testing.T) {
	const want = "https://download.nexusdock.co/latest/agentdock-component-catalog.json"
	if defaultCatalogURL != want {
		t.Fatalf("default catalog URL = %q, want %q", defaultCatalogURL, want)
	}
}

func TestStoreStatusDoesNotAdoptLegacyOrPath(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "bin", binaryFilename(runtime.GOOS))
	writeFakeCloudflared(t, legacy, "2026.9.3", "legacy")
	t.Setenv("PATH", filepath.Dir(legacy)+string(os.PathListSeparator)+os.Getenv("PATH"))

	status := store.Status()
	if status.State != "not_installed" || status.Installed || status.Ready {
		t.Fatalf("status unexpectedly adopted legacy/PATH binary: %+v", status)
	}
	if _, err := os.Stat(store.activePath()); !os.IsNotExist(err) {
		t.Fatalf("status mutated active pointer: %v", err)
	}
}

func TestCommitStagedActivatesOnlyAfterVerifiedVersionDirectory(t *testing.T) {
	requireCloudflaredRuntimePlatform(t)
	stubPlatformTrust(t)
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.root, 0o700); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(mustTempDir(t, store.root, ".staging-test-*"), binaryFilename(runtime.GOOS))
	writeFakeCloudflared(t, staged, "2026.9.3", "first")
	digest := mustSHA256(t, staged)

	if err := store.commitStaged(context.Background(), staged, "2026.9.3", digest); err != nil {
		t.Fatal(err)
	}
	status := store.Status()
	if !status.Ready || status.Version != "2026.9.3" || !strings.EqualFold(status.SHA256, digest) {
		t.Fatalf("unexpected active status: %+v", status)
	}
	if filepath.Clean(status.Path) != filepath.Clean(store.binaryPath("2026.9.3")) {
		t.Fatalf("active path = %q", status.Path)
	}
}

func TestCommitStagedRepairsCorruptSameVersionWithoutInPlaceOverwrite(t *testing.T) {
	requireCloudflaredRuntimePlatform(t)
	stubPlatformTrust(t)
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(store.root, 0o700); err != nil {
		t.Fatal(err)
	}

	first := filepath.Join(mustTempDir(t, store.root, ".staging-first-*"), binaryFilename(runtime.GOOS))
	writeFakeCloudflared(t, first, "2026.9.3", "first")
	firstDigest := mustSHA256(t, first)
	if err := store.commitStaged(context.Background(), first, "2026.9.3", firstDigest); err != nil {
		t.Fatal(err)
	}
	activePath := store.binaryPath("2026.9.3")
	if err := os.WriteFile(activePath, []byte("#!/bin/sh\necho corrupt\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if status := store.Status(); status.State != "broken" {
		t.Fatalf("corrupted component state = %+v", status)
	}

	repair := filepath.Join(mustTempDir(t, store.root, ".staging-repair-*"), binaryFilename(runtime.GOOS))
	writeFakeCloudflared(t, repair, "2026.9.3", "repaired")
	repairDigest := mustSHA256(t, repair)
	if err := store.commitStaged(context.Background(), repair, "2026.9.3", repairDigest); err != nil {
		t.Fatal(err)
	}
	status := store.Status()
	if !status.Ready || !strings.EqualFold(status.SHA256, repairDigest) || strings.EqualFold(status.SHA256, firstDigest) {
		t.Fatalf("repair did not atomically activate new content: %+v", status)
	}
	backups, err := filepath.Glob(filepath.Join(store.root, "versions", ".repair-backup-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("repair backup was not cleaned: %v", backups)
	}
}

func TestSelectCloudflaredArtifactRequiresPinnedOfficialUpstream(t *testing.T) {
	digest := strings.Repeat("a", 64)
	catalog := Catalog{
		SchemaVersion: 1,
		Components: []CatalogComponent{{
			Component:       CloudflaredName,
			Version:         "2026.9.3",
			UpstreamVersion: "2026.9.3",
			UpstreamSource:  "https://github.com/cloudflare/cloudflared/releases/tag/2026.9.3",
			Artifacts: []CatalogArtifact{{
				OS: "windows", Arch: "amd64", Format: "binary",
				URL:    "https://github.com/cloudflare/cloudflared/releases/download/2026.9.3/cloudflared-windows-amd64.exe",
				SHA256: digest,
			}},
		}},
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	entry, artifact, err := selectCloudflaredArtifact(data, "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Version != "2026.9.3" || artifact.SHA256 != digest || artifact.Format != "binary" {
		t.Fatalf("unexpected catalog selection: %+v %+v", entry, artifact)
	}

	for _, invalid := range []string{
		"http://github.com/cloudflare/cloudflared/releases/download/2026.9.3/cloudflared-windows-amd64.exe",
		"https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe",
		"https://github.com/uvwt/agentdock/releases/download/v1/cloudflared-windows-amd64.exe",
		"https://download.nexusdock.co/releases/v1/cloudflared-windows-amd64.exe",
		"https://github.com/cloudflare/cloudflared/releases/download/2026.9.3/cloudflared-windows-amd64.exe?x=1",
	} {
		catalog.Components[0].Artifacts[0].URL = invalid
		data, _ = json.Marshal(catalog)
		if _, _, err := selectCloudflaredArtifact(data, "windows", "amd64"); err == nil {
			t.Fatalf("unsafe artifact URL unexpectedly accepted: %s", invalid)
		}
	}
}

func TestExtractCloudflaredTGZAcceptsOnlySingleRegularBinary(t *testing.T) {
	valid := makeCloudflaredTGZ(t, []tar.Header{{Name: "cloudflared", Mode: 0o755, Size: 2, Typeflag: tar.TypeReg}}, [][]byte{[]byte("ok")})
	target := filepath.Join(t.TempDir(), "cloudflared")
	if err := extractCloudflaredTGZ(valid, target); err != nil {
		t.Fatalf("extract valid tgz: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ok" {
		t.Fatalf("extracted content = %q, want ok", data)
	}
}

func TestExtractCloudflaredTGZRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name    string
		headers []tar.Header
		bodies  [][]byte
	}{
		{name: "symlink", headers: []tar.Header{{Name: "cloudflared", Typeflag: tar.TypeSymlink, Linkname: "/tmp/evil"}}},
		{name: "hardlink", headers: []tar.Header{{Name: "cloudflared", Typeflag: tar.TypeLink, Linkname: "/tmp/evil"}}},
		{name: "parent traversal", headers: []tar.Header{{Name: "../cloudflared", Typeflag: tar.TypeReg}}},
		{name: "normalized traversal", headers: []tar.Header{{Name: "dir/../cloudflared", Typeflag: tar.TypeReg}}},
		{name: "absolute path", headers: []tar.Header{{Name: "/cloudflared", Typeflag: tar.TypeReg}}},
		{name: "backslash path", headers: []tar.Header{{Name: "..\\cloudflared", Typeflag: tar.TypeReg}}},
		{
			name: "extra entry",
			headers: []tar.Header{
				{Name: "cloudflared", Typeflag: tar.TypeReg},
				{Name: "README", Typeflag: tar.TypeReg},
			},
		},
		{
			name: "duplicate",
			headers: []tar.Header{
				{Name: "cloudflared", Typeflag: tar.TypeReg},
				{Name: "cloudflared", Typeflag: tar.TypeReg},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := makeCloudflaredTGZ(t, tt.headers, tt.bodies)
			if err := extractCloudflaredTGZ(archive, filepath.Join(t.TempDir(), "cloudflared")); err == nil {
				t.Fatal("unsafe tgz unexpectedly accepted")
			}
		})
	}
}

func makeCloudflaredTGZ(t *testing.T, headers []tar.Header, bodies [][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for i := range headers {
		header := headers[i]
		if header.Mode == 0 {
			header.Mode = 0o755
		}
		if header.Typeflag == 0 {
			header.Typeflag = tar.TypeReg
		}
		var body []byte
		if i < len(bodies) {
			body = bodies[i]
		}
		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			header.Size = int64(len(body))
		}
		if err := tarWriter.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if len(body) > 0 {
			if _, err := tarWriter.Write(body); err != nil {
				t.Fatal(err)
			}
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

func stubPlatformTrust(t *testing.T) {
	t.Helper()
	original := platformTrustVerifier
	platformTrustVerifier = func(context.Context, string, bool) error { return nil }
	t.Cleanup(func() { platformTrustVerifier = original })
}

func requireCloudflaredRuntimePlatform(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("cloudflared optional component runtime is supported only on macOS and Windows")
	}
}

func mustTempDir(t *testing.T, parent, pattern string) string {
	t.Helper()
	dir, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeFakeCloudflared(t *testing.T, path, version, marker string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fake is only used by Unix unit tests")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"cloudflared version " + version + " (" + marker + ")\"; exit 0; fi\nexit 0\n"
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
