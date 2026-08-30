//go:build windows

package app

import (
	"testing"
)

func TestWindowsFileToolSchemasExposeWSLRuntime(t *testing.T) {
	for _, name := range []string{"read_file", "list_dir", "search_text", "file_edit"} {
		properties := testInputSchema(name)["properties"].(map[string]any)
		runtimeProperty, ok := properties["runtime"].(map[string]any)
		if !ok {
			t.Fatalf("%s schema is missing runtime: %#v", name, properties)
		}
		enum, ok := runtimeProperty["enum"].([]string)
		if !ok || len(enum) != 2 || enum[0] != "windows" || enum[1] != "wsl" {
			t.Fatalf("%s runtime enum = %#v", name, runtimeProperty["enum"])
		}
		if _, ok := properties["wsl_distribution"]; !ok {
			t.Fatalf("%s schema is missing wsl_distribution", name)
		}

		outputProperties := testOutputSchema(name)["properties"].(map[string]any)
		if _, ok := outputProperties["runtime"]; !ok {
			t.Fatalf("%s output schema is missing runtime", name)
		}
		if _, ok := outputProperties["wsl_distribution"]; !ok {
			t.Fatalf("%s output schema is missing wsl_distribution", name)
		}
	}
}
