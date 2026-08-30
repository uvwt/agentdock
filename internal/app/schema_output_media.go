package app

func mediaOutputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "file_publish":
		props["artifact_id"] = schemaStringProperty("Published artifact id.")
		props["url"] = schemaStringProperty("Optional temporary signed download URL when a reachable base URL is available.")
		props["expires_at"] = schemaStringProperty("Signed URL expiry timestamp.")
		props["sha256"] = schemaStringProperty("Snapshot payload SHA-256.")
		props["size_bytes"] = schemaIntegerProperty("Snapshot payload size in bytes.")
		props["mime_type"] = schemaStringProperty("Payload media type.")
		props["filename"] = schemaStringProperty("Download filename used for Content-Disposition and signature verification.")
		props["archive"] = schemaBooleanProperty("Whether the source directory was packaged as tar.gz.")
		props["width"] = schemaIntegerProperty("Image width when the payload is an image.")
		props["height"] = schemaIntegerProperty("Image height when the payload is an image.")
	case "view_image":
		props["source"] = schemaOpenObjectProperty("Resolved artifact, path, or URL source metadata.")
		props["image"] = schemaOpenObjectProperty("Processed image metadata attached as standard MCP image content.")
		props["original"] = schemaOpenObjectProperty("Original/crop metadata.")
		props["resized"] = schemaBooleanProperty("Whether image bytes changed due to crop/resize/re-encode.")
		props["warnings"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	}
	return finalizeOutputSchema(props, required)
}
