package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMirrorManifestUsesReleaseCatalogAndVersionedBaseURL(t *testing.T) {
	dir := t.TempDir()
	for _, artifact := range ReleaseCatalog() {
		if err := os.WriteFile(filepath.Join(dir, artifact.Name), []byte(artifact.Name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var output bytes.Buffer
	if err := writeMirrorManifest("v1.2.3", "https://download.nexusdock.co/releases/v1.2.3/", dir, &output); err != nil {
		t.Fatal(err)
	}

	var manifest mirrorRelease
	if err := json.Unmarshal(output.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.TagName != "v1.2.3" {
		t.Fatalf("tag_name = %q, want v1.2.3", manifest.TagName)
	}
	if len(manifest.Assets) != len(ReleaseCatalog()) {
		t.Fatalf("asset count = %d, want %d", len(manifest.Assets), len(ReleaseCatalog()))
	}
	for i, artifact := range ReleaseCatalog() {
		asset := manifest.Assets[i]
		if asset.Name != artifact.Name {
			t.Fatalf("asset[%d].name = %q, want %q", i, asset.Name, artifact.Name)
		}
		wantURL := "https://download.nexusdock.co/releases/v1.2.3/" + artifact.Name
		if asset.URL != wantURL {
			t.Fatalf("asset[%d].url = %q, want %q", i, asset.URL, wantURL)
		}
	}
}

func TestMirrorManifestRejectsMissingAsset(t *testing.T) {
	dir := t.TempDir()
	for _, artifact := range ReleaseCatalog() {
		if artifact.Name == "agentdock_linux_amd64.tar.gz" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, artifact.Name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err := writeMirrorManifest("v1.2.3", "https://download.nexusdock.co/releases/v1.2.3", dir, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "agentdock_linux_amd64.tar.gz") {
		t.Fatalf("missing asset error = %v", err)
	}
}

func TestMirrorManifestRejectsUnsafeInputs(t *testing.T) {
	dir := t.TempDir()
	if err := writeMirrorManifest("../v1.2.3", "https://download.nexusdock.co/releases/v1.2.3", dir, &bytes.Buffer{}); err == nil {
		t.Fatal("tag with path separator must fail")
	}
	if err := writeMirrorManifest("v1.2.3", "http://download.nexusdock.co/releases/v1.2.3", dir, &bytes.Buffer{}); err == nil {
		t.Fatal("non-HTTPS public base URL must fail")
	}
}

func TestPrepareMirrorBootstrapUsesR2ReleaseBaseAndRefreshesChecksum(t *testing.T) {
	dir := t.TempDir()
	installPath := filepath.Join(dir, "install.sh")
	content := "#!/bin/sh\nDEFAULT_BASE_URL=\"https://github.com/uvwt/agentdock/releases/download/v1.2.3\"\necho ok\n"
	if err := os.WriteFile(installPath, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install.sh.sha256"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	componentAsset := filepath.Join(dir, "cloudflared_darwin_amd64")
	if err := os.WriteFile(componentAsset, []byte("component"), 0o755); err != nil {
		t.Fatal(err)
	}
	componentCatalog := `{
  "schema_version": 1,
  "components": [{
    "component": "cloudflared",
    "version": "2026.9.1",
    "upstream_version": "2026.9.1",
    "upstream_source": "https://github.com/cloudflare/cloudflared/releases/tag/2026.9.1",
    "artifacts": [{
      "os": "darwin",
      "arch": "amd64",
      "url": "https://github.com/uvwt/agentdock/releases/download/v1.2.3/cloudflared_darwin_amd64",
      "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    }]
  }]
}`
	if err := os.WriteFile(filepath.Join(dir, "agentdock-component-catalog.json"), []byte(componentCatalog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agentdock-component-catalog.json.sha256"), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	const baseURL = "https://download.nexusdock.co/releases/v1.2.3"
	if err := prepareMirrorBootstrap(baseURL, dir); err != nil {
		t.Fatal(err)
	}

	updated, err := os.ReadFile(installPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), `DEFAULT_BASE_URL="`+baseURL+`"`) {
		t.Fatalf("install.sh was not rewritten to %s", baseURL)
	}

	sum, err := fileSHA256(installPath)
	if err != nil {
		t.Fatal(err)
	}
	checksum, err := os.ReadFile(filepath.Join(dir, "install.sh.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if string(checksum) != sum+"  install.sh\n" {
		t.Fatalf("install.sh.sha256 = %q, want refreshed checksum", string(checksum))
	}

	catalogData, err := os.ReadFile(filepath.Join(dir, "agentdock-component-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(catalogData), baseURL+"/cloudflared_darwin_amd64") {
		t.Fatalf("component catalog was not rewritten to the R2 versioned base: %s", catalogData)
	}
	catalogSum, err := fileSHA256(filepath.Join(dir, "agentdock-component-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalogChecksum, err := os.ReadFile(filepath.Join(dir, "agentdock-component-catalog.json.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if string(catalogChecksum) != catalogSum+"  agentdock-component-catalog.json\n" {
		t.Fatalf("component catalog checksum was not refreshed: %q", catalogChecksum)
	}
}

func TestPrepareMirrorBootstrapRequiresDefaultBase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := prepareMirrorBootstrap("https://download.nexusdock.co/releases/v1.2.3", dir)
	if err == nil || !strings.Contains(err.Error(), "DEFAULT_BASE_URL") {
		t.Fatalf("missing DEFAULT_BASE_URL error = %v", err)
	}
}
