//go:build !windows

package app

import (
	"strings"
	"testing"
)

func TestFileEditPatchSchemaDescribesStructuredEnvelope(t *testing.T) {
	properties := testInputSchema("file_edit")["properties"].(map[string]any)
	patch := properties["patch"].(map[string]any)
	description, _ := patch["description"].(string)
	for _, marker := range []string{"*** Begin Patch", "*** Update File: <path>", "@@ [<optional context line>]", "*** End of File"} {
		if !strings.Contains(description, marker) {
			t.Fatalf("file_edit patch description missing %q: %q", marker, description)
		}
	}
}

func TestNonWindowsFileToolSchemasDoNotExposeWSLRuntime(t *testing.T) {
	for _, name := range []string{"read_file", "list_dir", "search_text", "file_edit"} {
		properties := testInputSchema(name)["properties"].(map[string]any)
		if _, ok := properties["runtime"]; ok {
			t.Fatalf("%s input schema unexpectedly exposes runtime", name)
		}
		if _, ok := properties["wsl_distribution"]; ok {
			t.Fatalf("%s input schema unexpectedly exposes wsl_distribution", name)
		}
		outputProperties := testOutputSchema(name)["properties"].(map[string]any)
		if _, ok := outputProperties["runtime"]; ok {
			t.Fatalf("%s output schema unexpectedly exposes runtime", name)
		}
	}
}
