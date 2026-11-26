//go:build windows

package selfupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestWindowsInstallerArgsRespectInteractiveUI(t *testing.T) {
	installRoot := `C:\Users\demo\AppData\Local\AgentDock`

	interactive := windowsInstallerArgs(installRoot, true)
	for _, want := range []string{"/SILENT", "/NORESTART", "/CLOSEAPPLICATIONS", "/DIR=" + installRoot} {
		if !slices.Contains(interactive, want) {
			t.Fatalf("interactive installer args missing %q: %v", want, interactive)
		}
	}
	if slices.Contains(interactive, "/SUPPRESSMSGBOXES") {
		t.Fatalf("interactive UI update must not suppress error message boxes: %v", interactive)
	}

	unattended := windowsInstallerArgs(installRoot, false)
	if !slices.Contains(unattended, "/SUPPRESSMSGBOXES") {
		t.Fatalf("unattended update should suppress message boxes: %v", unattended)
	}
}

func TestWriteWindowsInstallerHandoffMarker(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)

	markerPath, err := writeWindowsInstallerHandoffMarker(root, "1.1.0", "1.1.1", now)
	if err != nil {
		t.Fatalf("writeWindowsInstallerHandoffMarker failed: %v", err)
	}
	wantPath := filepath.Join(root, "update", windowsInstallerHandoffFile)
	if markerPath != wantPath {
		t.Fatalf("markerPath = %q, want %q", markerPath, wantPath)
	}

	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	var marker windowsInstallerHandoffMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatalf("unmarshal marker: %v", err)
	}
	if marker.SchemaVersion != windowsInstallerHandoffVersion ||
		marker.SourceVersion != "v1.1.0" ||
		marker.TargetVersion != "v1.1.1" ||
		marker.CreatedAt != "2026-04-15T12:00:00Z" {
		t.Fatalf("unexpected marker payload: %#v", marker)
	}
}
