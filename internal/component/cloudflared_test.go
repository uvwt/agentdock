package component

import (
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

func TestSelectCloudflaredArtifactRequiresPinnedHTTPSDigest(t *testing.T) {
	digest := strings.Repeat("a", 64)
	catalog := Catalog{
		SchemaVersion: 1,
		Components: []CatalogComponent{{
			Component:       CloudflaredName,
			Version:         "2026.9.3",
			UpstreamVersion: "2026.9.3",
			UpstreamSource:  "https://github.com/cloudflare/cloudflared/releases/tag/2026.9.3",
			Artifacts: []CatalogArtifact{{
				OS: "windows", Arch: "amd64",
				URL:    "https://github.com/uvwt/agentdock/releases/download/v1/cloudflared-windows-amd64.exe",
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
	if entry.Version != "2026.9.3" || artifact.SHA256 != digest {
		t.Fatalf("unexpected catalog selection: %+v %+v", entry, artifact)
	}

	catalog.Components[0].Artifacts[0].URL = "http://example.invalid/cloudflared.exe"
	data, _ = json.Marshal(catalog)
	if _, _, err := selectCloudflaredArtifact(data, "windows", "amd64"); err == nil {
		t.Fatal("HTTP artifact URL unexpectedly accepted")
	}
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
