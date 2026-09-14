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
		"install-linux-platform.sh":    false,
		"install-macos-platform.sh":    false,
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

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
