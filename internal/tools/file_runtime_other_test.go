//go:build !windows

package tools

import "testing"

func TestNonWindowsFileToolSchemasDoNotExposeWSLRuntime(t *testing.T) {
	for _, name := range []string{"read_file", "list_dir", "list_files", "search_text", "file_edit"} {
		properties := InputSchema(name)["properties"].(map[string]any)
		if _, ok := properties["runtime"]; ok {
			t.Fatalf("%s input schema unexpectedly exposes runtime", name)
		}
		if _, ok := properties["wsl_distribution"]; ok {
			t.Fatalf("%s input schema unexpectedly exposes wsl_distribution", name)
		}
		outputProperties := OutputSchema(name)["properties"].(map[string]any)
		if _, ok := outputProperties["runtime"]; ok {
			t.Fatalf("%s output schema unexpectedly exposes runtime", name)
		}
	}
}

func TestNonWindowsFileRuntimeRejectsWSLSelection(t *testing.T) {
	if _, err := selectFileRuntime(map[string]any{"runtime": "wsl"}); err == nil {
		t.Fatal("expected non-Windows file runtime override to be rejected")
	}
	if _, err := selectFileRuntime(map[string]any{"wsl_distribution": "Ubuntu"}); err == nil {
		t.Fatal("expected non-Windows WSL distribution to be rejected")
	}
}
