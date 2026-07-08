package mcp

import "testing"

func TestToolDescriptorsUseRegistryAnnotations(t *testing.T) {
	descriptors := toolDescriptorsForNames([]string{"read_file", "skill_manage"})
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
	assertAnnotation("skill_manage", "readOnlyHint", false)
	assertAnnotation("skill_manage", "destructiveHint", true)
	assertAnnotation("skill_manage", "openWorldHint", true)
}

func TestArtifactToolDescriptorsExposeFileRewritePaths(t *testing.T) {
	descriptors := toolDescriptorsForNames([]string{"artifact_send", "artifact_fetch_download"})
	byName := map[string]map[string]any{}
	for _, descriptor := range descriptors {
		name, _ := descriptor["name"].(string)
		byName[name] = descriptor
	}
	args, ok := byName["artifact_send"]["file_arg_rewrite_paths"].([]string)
	if !ok || len(args) != 1 || args[0] != "file" {
		t.Fatalf("artifact_send file_arg_rewrite_paths = %#v", byName["artifact_send"]["file_arg_rewrite_paths"])
	}
	results, ok := byName["artifact_fetch_download"]["file_result_rewrite_paths"].([]string)
	if !ok || len(results) != 1 || results[0] != "file_path" {
		t.Fatalf("artifact_fetch_download file_result_rewrite_paths = %#v", byName["artifact_fetch_download"]["file_result_rewrite_paths"])
	}
	meta, ok := byName["artifact_send"]["_meta"].(map[string]any)
	if !ok || meta["file_arg_rewrite_paths"] == nil {
		t.Fatalf("artifact_send _meta missing: %#v", meta)
	}
}

func TestArtifactFetchDownloadEnvelopeReturnsResourceLink(t *testing.T) {
	result := toolEnvelope("artifact_fetch_download", map[string]any{
		"mounted": false, "resource_uri": "https://example.test/artifacts/fetch/fet_test?token=opaque",
		"file_name": "report.txt", "mime_type": "text/plain", "size": int64(12),
	}, nil)
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content = %#v", result["content"])
	}
	if content[0]["type"] != "resource_link" || content[0]["name"] != "report.txt" {
		t.Fatalf("resource link = %#v", content[0])
	}
}
