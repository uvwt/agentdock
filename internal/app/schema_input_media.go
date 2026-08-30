package app

func mediaInputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "file_publish":
		props["file"] = map[string]any{"type": "string", "format": "binary", "description": "Top-level file parameter. Connector runtimes should pass the mounted local path when available."}
		props["path"] = schemaStringProperty("Local file or directory path visible to this AgentDock instance. Relative paths resolve from ~/AgentDock.")
		props["retention_seconds"] = schemaIntegerProperty("Signed URL retention in seconds. Defaults to 86400 and is capped at 604800.")
		required = []string{}
	case "view_image":
		props["artifact_id"] = schemaStringProperty("Artifact id returned by an AgentDock image-producing tool.")
		props["path"] = schemaStringProperty("Host image path. Relative paths resolve from ~/AgentDock.")
		props["url"] = schemaStringProperty("Absolute HTTP(S) image URL.")
		props["max_source_bytes"] = schemaIntegerProperty("Maximum source bytes before processing. Defaults to 20971520 and is capped at 104857600.")
		props["source_timeout_ms"] = schemaBoundedIntegerProperty("HTTP(S) source timeout in milliseconds. Defaults to 15000 and is capped at 120000.", 1, 120000)
		props["max_bytes"] = schemaIntegerProperty("Maximum processed image bytes returned to the model. Defaults to 750000 and is capped at 2097152.")
		props["max_width"] = schemaIntegerProperty("Maximum image width. Defaults to 1280.")
		props["max_height"] = schemaIntegerProperty("Maximum image height. Defaults to 1280.")
		props["auto_resize"] = schemaBooleanProperty("Resize/compress when limits are exceeded. Defaults to true.")
		props["format"] = schemaStringProperty("Processed image format: jpeg or png. Defaults to jpeg.")
		props["quality"] = schemaIntegerProperty("JPEG quality when format is jpeg. Defaults to 72.")
		props["crop"] = map[string]any{"type": "object", "description": "Optional crop rectangle {x,y,width,height} before resizing.", "additionalProperties": true}
	}
	schema := finalizeInputSchema(name, props, required)
	if name == "view_image" {
		// 严格的工具调用供应商会独立校验 oneOf 分支类型；每个分支都显式声明对象类型。
		schema["oneOf"] = []map[string]any{
			{"type": "object", "required": []string{"artifact_id"}},
			{"type": "object", "required": []string{"path"}},
			{"type": "object", "required": []string{"url"}},
		}
	}
	return schema
}
