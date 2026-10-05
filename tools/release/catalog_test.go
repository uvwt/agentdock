package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/buildinfo"
)

func TestReleaseCatalogKeepsPublicInstallerEntries(t *testing.T) {
	catalog := ReleaseCatalog()
	required := map[string]bool{
		"install.sh":                   false,
		"install.ps1":                  false,
		"agentdock_linux_amd64.tar.gz": false,
		"AgentDockSetup-amd64.exe":     false,
	}
	for _, artifact := range catalog {
		if _, ok := required[artifact.Name]; ok {
			required[artifact.Name] = true
		}
		if strings.HasSuffix(artifact.Name, ".tar.gz") || strings.HasSuffix(artifact.Name, ".zip") {
			found := false
			for _, candidate := range catalog {
				if candidate.Name == artifact.Name+".sha256" && candidate.Kind == "checksum" {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("archive %s missing checksum artifact", artifact.Name)
			}
		}
	}
	for name, seen := range required {
		if !seen {
			t.Fatalf("release catalog missing %s", name)
		}
	}
	var publicScripts []string
	for _, artifact := range catalog {
		if artifact.PublicContract && artifact.Kind == "bootstrap" {
			publicScripts = append(publicScripts, artifact.Name)
		}
		if artifact.PublicContract && artifact.Kind == "runtime-adapter" {
			t.Fatalf("runtime adapter must not be a public Release contract: %s", artifact.Name)
		}
	}
	if strings.Join(publicScripts, ",") != "install.sh,install.ps1" {
		t.Fatalf("public bootstrap scripts = %v, want [install.sh install.ps1]", publicScripts)
	}
}

func TestVerifyDistRequiresCatalogArtifacts(t *testing.T) {
	dir := t.TempDir()
	if err := run([]string{"verify-dist", dir}, discard{}); err == nil {
		t.Fatal("empty dist must fail")
	}
	for _, artifact := range ReleaseCatalog() {
		if !artifact.Required {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, artifact.Name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := run([]string{"verify-dist", dir}, discard{}); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyVersionMatchesBuildInfo(t *testing.T) {
	if err := run([]string{"verify-version", "v" + strings.TrimPrefix(buildinfo.Version, "v")}, discard{}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"verify-version", "v0.0.0"}, discard{}); err == nil {
		t.Fatal("expected version mismatch")
	}
}

func TestReleaseMetadataClassifiesPrereleaseAndStable(t *testing.T) {
	rc, err := releaseMetadataForTag("v1.0.0-rc.2")
	if err != nil {
		t.Fatal(err)
	}
	if !rc.Prerelease || rc.Core != "1.0.0" || rc.Version != "1.0.0-rc.2" {
		t.Fatalf("unexpected prerelease metadata: %+v", rc)
	}
	stable, err := releaseMetadataForTag("v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if stable.Prerelease || stable.Core != "1.0.0" || stable.Version != "1.0.0" {
		t.Fatalf("unexpected stable metadata: %+v", stable)
	}
	for _, invalid := range []string{"1.0.0", "v1.0", "v1.0.0-"} {
		if _, err := releaseMetadataForTag(invalid); err == nil {
			t.Fatalf("invalid release tag unexpectedly accepted: %s", invalid)
		}
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestCloudflaredComponentCatalogUsesPinnedOfficialMetadata(t *testing.T) {
	entry, err := currentCloudflaredCatalogEntry()
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := writeCloudflaredComponentCatalog(&output); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{
		`"schema_version": 2`,
		`"revision": 1`,
		`"status": "supported"`,
		`"min_version": "0.9.1"`,
		`"max_version_exclusive": "2.0.0"`,
		`"version": "` + entry.Version + `"`,
		`"upstream_source": "https://github.com/cloudflare/cloudflared/releases/tag/` + entry.Version + `"`,
		`"format": "binary"`,
		`"format": "tgz"`,
		`https://github.com/cloudflare/cloudflared/releases/download/` + entry.Version + `/cloudflared-windows-amd64.exe`,
		`https://github.com/cloudflare/cloudflared/releases/download/` + entry.Version + `/cloudflared-darwin-arm64.tgz`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("catalog missing %q: %s", want, text)
		}
	}
	for _, forbidden := range []string{
		"github.com/uvwt/agentdock/releases",
		"download.nexusdock.co",
		"/latest/",
		"cloudflared_darwin_",
		"cloudflared_windows_amd64.exe",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("catalog must not rehost cloudflared; found %q in %s", forbidden, text)
		}
	}
}

func TestReleaseCatalogDoesNotRequireCloudflaredBinary(t *testing.T) {
	for _, artifact := range ReleaseCatalog() {
		if strings.HasPrefix(artifact.Name, "cloudflared_") || strings.HasPrefix(artifact.Name, "cloudflared-") {
			t.Fatalf("AgentDock Release must not contain cloudflared binary: %+v", artifact)
		}
	}
}
