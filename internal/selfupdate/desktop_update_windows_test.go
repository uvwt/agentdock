//go:build windows

package selfupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractWindowsDesktopUpdateArchive(t *testing.T) {
	archive := makeWindowsDesktopArchive(t, map[string][]byte{
		"agentdock.exe":      []byte("core"),
		"agentdock-tray.exe": []byte("tray-new"),
		"agentdock.ico":      []byte("icon-new"),
		"manage-windows.ps1": []byte("manager-new"),
	})

	root, err := extractDesktopUpdateArchive(context.Background(), archive, t.TempDir(), "0.7.5")
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]string{
		"agentdock-tray.exe":      "tray-new",
		"agentdock.ico":           "icon-new",
		"manage-windows.ps1":      "manager-new",
		windowsDesktopVersionFile: "v0.7.5\n",
	} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(data) != expected {
			t.Fatalf("%s = %q, want %q", name, data, expected)
		}
	}
}

func TestExtractWindowsDesktopUpdateArchiveRequiresWholeDesktopPayload(t *testing.T) {
	archive := makeWindowsDesktopArchive(t, map[string][]byte{
		"agentdock-tray.exe": []byte("tray"),
		"agentdock.ico":      []byte("icon"),
	})
	_, err := extractDesktopUpdateArchive(context.Background(), archive, t.TempDir(), "0.7.5")
	if err == nil || !strings.Contains(err.Error(), "manage-windows.ps1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractWindowsDesktopUpdateArchiveKeepsGenerationWSLHelper(t *testing.T) {
	archive := makeWindowsDesktopArchive(t, map[string][]byte{
		"agentdock-tray.exe":                          []byte("tray"),
		"agentdock.ico":                               []byte("icon"),
		"manage-windows.ps1":                          []byte("manager"),
		"agentdock-arbiter.exe":                       []byte("arbiter"),
		"agentdock-shim.exe":                          []byte("shim"),
		"agentdock-tray-shim.exe":                     []byte("tray-shim"),
		"wsl-helper/manifest.json":                    []byte(`{"protocol_version":"1"}`),
		"wsl-helper/agentdock-wsl-helper-linux-amd64": []byte("linux-amd64"),
		"wsl-helper/agentdock-wsl-helper-linux-arm64": []byte("linux-arm64"),
	})

	root, err := extractDesktopUpdateArchive(context.Background(), archive, t.TempDir(), "0.9.0")
	if err != nil {
		t.Fatal(err)
	}
	for relative, expected := range map[string]string{
		"wsl-helper/manifest.json":                    `{"protocol_version":"1"}`,
		"wsl-helper/agentdock-wsl-helper-linux-amd64": "linux-amd64",
		"wsl-helper/agentdock-wsl-helper-linux-arm64": "linux-arm64",
	} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		if string(data) != expected {
			t.Fatalf("%s = %q, want %q", relative, data, expected)
		}
	}
}

func TestWindowsDesktopUpdateVersionMarker(t *testing.T) {
	root := t.TempDir()
	if got := desktopUpdateVersion(root); got != "" {
		t.Fatalf("missing marker version = %q", got)
	}
	if err := os.WriteFile(filepath.Join(root, windowsDesktopVersionFile), []byte("0.7.5\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := desktopUpdateVersion(root); got != "v0.7.5" {
		t.Fatalf("desktop version = %q, want v0.7.5", got)
	}
}

func makeWindowsDesktopArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, data := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o644)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
