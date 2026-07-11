package mcp

import "testing"

func TestToolDescriptorsDoNotExposePermissionAnnotations(t *testing.T) {
	descriptors := toolDescriptorsForNames([]string{"read_file", "skill_package"})
	byName := map[string]map[string]any{}
	for _, descriptor := range descriptors {
		name, _ := descriptor["name"].(string)
		byName[name] = descriptor
	}
	for _, tool := range []string{"read_file", "skill_package"} {
		if _, ok := byName[tool]["annotations"]; ok {
			t.Fatalf("%s should not expose permission annotations", tool)
		}
	}
}

func TestFilePublishDescriptorExposesFileRewritePath(t *testing.T) {
	descriptors := toolDescriptorsForNames([]string{"file_publish"})
	byName := map[string]map[string]any{}
	for _, descriptor := range descriptors {
		name, _ := descriptor["name"].(string)
		byName[name] = descriptor
	}
	args, ok := byName["file_publish"]["file_arg_rewrite_paths"].([]string)
	if !ok || len(args) != 1 || args[0] != "file" {
		t.Fatalf("file_publish file_arg_rewrite_paths = %#v", byName["file_publish"]["file_arg_rewrite_paths"])
	}
	meta, ok := byName["file_publish"]["_meta"].(map[string]any)
	if !ok || meta["file_arg_rewrite_paths"] == nil || meta["openai/fileParams"] == nil {
		t.Fatalf("file_publish _meta missing: %#v", meta)
	}
}

func TestToolEnvelopeMCPImageStripsInternalBase64FromStructuredContent(t *testing.T) {
	response := toolEnvelope("view_image", map[string]any{
		"ok":                   true,
		"return_mode":          "mcp_image",
		"_mcp_image_base64":    "abc123",
		"_mcp_image_mime_type": "image/png",
		"inline":               map[string]any{"mode": "mcp_image", "attached": true},
	}, nil)
	content := response["content"].([]map[string]any)
	if content[0]["type"] != "image" || content[0]["data"] != "abc123" || content[0]["mimeType"] != "image/png" {
		t.Fatalf("content = %#v", content)
	}
	structured := response["structuredContent"].(map[string]any)
	if _, ok := structured["_mcp_image_base64"]; ok {
		t.Fatalf("structuredContent leaked internal base64: %#v", structured)
	}
	if _, ok := structured["_mcp_image_mime_type"]; ok {
		t.Fatalf("structuredContent leaked internal mime type: %#v", structured)
	}
}
