package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareReleaseDistributionPinsOnlyFirstPartyBootstrap(t *testing.T) {
	dir := t.TempDir()
	installSH := "#!/bin/sh\nDEFAULT_BASE_URL=\"https://download.nexusdock.co/latest\"\nVERSIONED_RELEASE_BASE_URL=\"https://download.nexusdock.co/releases\"\n"
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte(installSH), 0o755); err != nil {
		t.Fatal(err)
	}
	installPS1 := "$defaultReleaseBaseUrl = 'https://download.nexusdock.co/latest'\n$versionedReleaseBaseUrl = 'https://download.nexusdock.co/releases'\n"
	if err := os.WriteFile(filepath.Join(dir, "install.ps1"), []byte(installPS1), 0o644); err != nil {
		t.Fatal(err)
	}

	const catalog = "{\"schema_version\":1,\"components\":[{\"component\":\"cloudflared\",\"artifacts\":[{\"url\":\"https://github.com/cloudflare/cloudflared/releases/download/2026.9.1/cloudflared-windows-amd64.exe\"}]}]}"
	catalogPath := filepath.Join(dir, "agentdock-component-catalog.json")
	if err := os.WriteFile(catalogPath, []byte(catalog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath+".sha256", []byte("catalog-checksum-owned-by-catalog-step\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	const releaseBaseURL = "https://download.nexusdock.co/releases/v1.2.3"
	if err := prepareReleaseDistribution(releaseBaseURL, dir); err != nil {
		t.Fatal(err)
	}

	shData, err := os.ReadFile(filepath.Join(dir, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shData), "DEFAULT_BASE_URL=\""+releaseBaseURL+"\"") {
		t.Fatalf("install.sh did not pin the release base: %s", shData)
	}
	if !strings.Contains(string(shData), "VERSIONED_RELEASE_BASE_URL=\"https://download.nexusdock.co/releases\"") {
		t.Fatalf("install.sh lost the canonical versioned release root: %s", shData)
	}

	psData, err := os.ReadFile(filepath.Join(dir, "install.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(psData), "$defaultReleaseBaseUrl = '"+releaseBaseURL+"'") {
		t.Fatalf("install.ps1 did not pin the release base: %s", psData)
	}
	if !strings.Contains(string(psData), "$versionedReleaseBaseUrl = 'https://download.nexusdock.co/releases'") {
		t.Fatalf("install.ps1 lost the canonical versioned release root: %s", psData)
	}

	catalogData, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(catalogData) != catalog {
		t.Fatalf("prepare-distribution changed third-party catalog metadata: %s", catalogData)
	}
	catalogChecksum, err := os.ReadFile(catalogPath + ".sha256")
	if err != nil {
		t.Fatal(err)
	}
	if string(catalogChecksum) != "catalog-checksum-owned-by-catalog-step\n" {
		t.Fatalf("prepare-distribution changed catalog checksum: %q", catalogChecksum)
	}

	for _, name := range []string{"install.sh", "install.ps1"} {
		sum, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		checksum, err := os.ReadFile(filepath.Join(dir, name+".sha256"))
		if err != nil {
			t.Fatal(err)
		}
		if string(checksum) != sum+"  "+name+"\n" {
			t.Fatalf("%s.sha256 = %q, want refreshed checksum", name, checksum)
		}
	}
}

func TestPrepareReleaseDistributionRequiresSourceLatestMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install.ps1"), []byte(sourcePowerShellBaseLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := prepareReleaseDistribution("https://download.nexusdock.co/releases/v1.2.3", dir)
	if err == nil || !strings.Contains(err.Error(), "发布基址") {
		t.Fatalf("missing source marker error = %v", err)
	}
}

func TestNormalizePublicBaseURLRejectsUnsafeInputs(t *testing.T) {
	for _, raw := range []string{
		"http://download.nexusdock.co/releases/v1.2.3",
		"https://download.nexusdock.co/releases/v1.2.3?token=x",
		"https://download.nexusdock.co/releases/v1.2.3#fragment",
	} {
		if _, err := normalizePublicBaseURL(raw); err == nil {
			t.Fatalf("normalizePublicBaseURL(%q) must fail", raw)
		}
	}
}
