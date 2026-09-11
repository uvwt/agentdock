//go:build windows

package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/workspace"
)

func newWSLHelperRuntimeTestService(t *testing.T) *Service {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(ws, nil, func(string, map[string]string) ([]string, error) {
		return os.Environ(), nil
	})
}

func TestLoadWSLHelperPayloadVerifiesDigest(t *testing.T) {
	directory := t.TempDir()
	name := "agentdock-wsl-helper-linux-amd64"
	payload := []byte("test helper payload")
	if err := os.WriteFile(filepath.Join(directory, name), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	item := wslHelperManifestItem{File: name, SHA256: hex.EncodeToString(digest[:])}
	_, _, loaded, err := loadWSLHelperPayload(directory, item)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded) != string(payload) {
		t.Fatalf("loaded payload = %q, want %q", loaded, payload)
	}

	if err := os.WriteFile(filepath.Join(directory, name), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadWSLHelperPayload(directory, item); err == nil {
		t.Fatal("tampered helper payload unexpectedly passed integrity verification")
	}
}

func TestWSLHelperDeploymentIntegration(t *testing.T) {
	payloadDirectory := strings.TrimSpace(os.Getenv("AGENTDOCK_WSL_HELPER_PAYLOAD_DIR"))
	if payloadDirectory == "" {
		t.Skip("AGENTDOCK_WSL_HELPER_PAYLOAD_DIR is required for WSL helper deployment integration")
	}
	wslPath, err := exec.LookPath("wsl.exe")
	if err != nil {
		t.Skip("wsl.exe is required for WSL helper deployment integration")
	}
	manifestBytes, err := os.ReadFile(filepath.Join(payloadDirectory, wslHelperManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var manifest wslHelperManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ProtocolVersion != wslHelperProtocolVersion {
		t.Fatalf("protocol = %q, want %q", manifest.ProtocolVersion, wslHelperProtocolVersion)
	}

	service := newWSLHelperRuntimeTestService(t)
	selection := fileRuntimeSelection{Runtime: "wsl"}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	architecture, err := service.detectWSLHelperArchitecture(ctx, wslPath, selection)
	if err != nil {
		t.Fatal(err)
	}
	item, ok := manifest.Helpers[architecture]
	if !ok {
		t.Fatalf("manifest does not contain architecture %q", architecture)
	}
	_, expectedHash, payload, err := loadWSLHelperPayload(payloadDirectory, item)
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := "/tmp/agentdock-wsl-helper-deploy-e2e-" + strings.ReplaceAll(time.Now().UTC().Format("20060102T150405.000000000"), ".", "")
	target := targetRoot + "/agentdock-wsl-helper"
	t.Cleanup(func() {
		_, _ = service.runWSLCommand(context.Background(), wslPath, selection, nil, "rm", "-rf", targetRoot)
	})

	if err := service.deployWSLHelper(ctx, wslPath, selection, target, expectedHash, payload); err != nil {
		t.Fatal(err)
	}
	deployedHash, err := service.probeWSLHelperHash(ctx, wslPath, selection, target)
	if err != nil {
		t.Fatal(err)
	}
	if deployedHash != expectedHash {
		t.Fatalf("deployed hash = %q, want %q", deployedHash, expectedHash)
	}
	if err := service.verifyWSLHelperProtocol(ctx, wslPath, selection, target); err != nil {
		t.Fatal(err)
	}
}
