package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestPrepareGitHubReleaseKeepsOnlyUserFacingAssets(t *testing.T) {
	dist := t.TempDir()
	output := filepath.Join(t.TempDir(), "github-release")

	var expected []string
	for _, artifact := range ReleaseCatalog() {
		if !artifact.GitHubRelease {
			continue
		}
		expected = append(expected, artifact.Name)
		if err := os.WriteFile(filepath.Join(dist, artifact.Name), []byte("payload:"+artifact.Name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(expected)

	var log strings.Builder
	if err := prepareGitHubRelease(dist, output, &log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "5 user-facing assets + SHA256SUMS.txt") {
		t.Fatalf("unexpected prepare log: %s", log.String())
	}

	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	sort.Strings(got)

	want := append(append([]string(nil), expected...), "SHA256SUMS.txt")
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("GitHub Release files = %v, want %v", got, want)
	}

	for _, forbidden := range []string{
		"AgentDock-macos-universal.zip",
		"agentdock_windows_amd64.zip",
		"agentdock_darwin_amd64.tar.gz",
		"install.sh",
		"install.ps1",
		"agentdock-component-catalog.json",
	} {
		if _, err := os.Stat(filepath.Join(output, forbidden)); !os.IsNotExist(err) {
			t.Fatalf("machine-only artifact leaked into GitHub Release: %s", forbidden)
		}
	}

	checksumData, err := os.ReadFile(filepath.Join(output, "SHA256SUMS.txt"))
	if err != nil {
		t.Fatal(err)
	}
	checksums := string(checksumData)
	for _, name := range expected {
		sum := sha256.Sum256([]byte("payload:" + name))
		wantLine := fmt.Sprintf("%x  %s\n", sum, name)
		if !strings.Contains(checksums, wantLine) {
			t.Fatalf("SHA256SUMS.txt missing %q", strings.TrimSpace(wantLine))
		}
	}
}

func TestGitHubReleaseCatalogIsDeliberatelySmall(t *testing.T) {
	var names []string
	for _, artifact := range ReleaseCatalog() {
		if artifact.GitHubRelease {
			names = append(names, artifact.Name)
		}
	}
	sort.Strings(names)
	want := []string{
		"AgentDock-macos-universal.dmg",
		"AgentDockSetup-amd64.exe",
		"AgentDockSetup-arm64.exe",
		"agentdock_linux_amd64.tar.gz",
		"agentdock_linux_arm64.tar.gz",
	}
	sort.Strings(want)
	if strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Fatalf("GitHub Release catalog = %v, want %v", names, want)
	}
}
