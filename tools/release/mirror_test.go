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
