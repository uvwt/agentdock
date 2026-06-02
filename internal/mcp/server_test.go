package mcp

import "testing"

func TestToolDescriptorsUseRegistryAnnotations(t *testing.T) {
	descriptors := toolDescriptorsForNames([]string{"read_file", "desktop_click", "plugin_call"})
	byName := map[string]map[string]any{}
	for _, descriptor := range descriptors {
		name, _ := descriptor["name"].(string)
		byName[name] = descriptor
	}

	assertAnnotation := func(tool string, key string, want bool) {
		t.Helper()
		annotations, ok := byName[tool]["annotations"].(map[string]any)
		if !ok {
			t.Fatalf("%s annotations missing or wrong type", tool)
		}
		if got, _ := annotations[key].(bool); got != want {
			t.Fatalf("%s %s = %v, want %v", tool, key, got, want)
		}
	}

	assertAnnotation("read_file", "readOnlyHint", true)
	assertAnnotation("read_file", "destructiveHint", false)
	assertAnnotation("read_file", "openWorldHint", false)
	assertAnnotation("desktop_click", "readOnlyHint", false)
	assertAnnotation("desktop_click", "destructiveHint", true)
	assertAnnotation("desktop_click", "openWorldHint", false)
	assertAnnotation("plugin_call", "readOnlyHint", false)
	assertAnnotation("plugin_call", "destructiveHint", false)
	assertAnnotation("plugin_call", "openWorldHint", true)
}
