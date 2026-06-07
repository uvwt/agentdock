package mcp

import "testing"

func TestToolDescriptorsUseRegistryAnnotations(t *testing.T) {
	descriptors := toolDescriptorsForNames([]string{"read_file", "desktop_act", "skill_manage"})
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
	assertAnnotation("desktop_act", "readOnlyHint", false)
	assertAnnotation("desktop_act", "destructiveHint", true)
	assertAnnotation("desktop_act", "openWorldHint", false)
	assertAnnotation("skill_manage", "readOnlyHint", false)
	assertAnnotation("skill_manage", "destructiveHint", true)
	assertAnnotation("skill_manage", "openWorldHint", true)
}
