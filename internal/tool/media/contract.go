package media

import (
	"time"

	"github.com/uvwt/agentdock/internal/publicartifacts"
	toolcontract "github.com/uvwt/agentdock/internal/tool/contract"
)

const (
	ToolViewImage   = "view_image"
	ToolFilePublish = "file_publish"
)

func InputSchema(name string) (map[string]any, bool) {
	stringProp := toolcontract.String
	intProp := toolcontract.Integer
	boolProp := toolcontract.Boolean
	boundedIntProp := toolcontract.BoundedInteger
	props := map[string]any{}
	var required []string

	switch name {
	case ToolViewImage:
		props["artifact_id"] = stringProp("Artifact id returned by an AgentDock image-producing tool.")
		props["path"] = stringProp("Host image path. Relative paths resolve from ~/AgentDock.")
		props["url"] = stringProp("Absolute HTTP(S) image URL.")
		props["max_source_bytes"] = boundedIntProp("Maximum source bytes before processing. Defaults to 20971520 and is capped at 104857600.", 1, hardImageSourceBytes)
		props["source_timeout_ms"] = boundedIntProp("HTTP(S) source timeout in milliseconds. Defaults to 15000 and is capped at 120000.", 1, 120000)
		props["max_bytes"] = boundedIntProp("Maximum processed image bytes returned to the model. Defaults to 750000 and is capped at 2097152.", 1, hardViewImageBytes)
		props["max_width"] = map[string]any{"type": "integer", "description": "Maximum image width. Defaults to 1280.", "minimum": 1}
		props["max_height"] = map[string]any{"type": "integer", "description": "Maximum image height. Defaults to 1280.", "minimum": 1}
		props["auto_resize"] = boolProp("Resize/compress when limits are exceeded. Defaults to true.")
		props["format"] = map[string]any{"type": "string", "description": "Processed image format. Defaults to jpeg.", "enum": []string{"jpeg", "png"}}
		props["quality"] = boundedIntProp("JPEG quality when format is jpeg. Defaults to 72.", 35, 95)
		props["crop"] = map[string]any{
			"type":                 "object",
			"description":          "Optional crop rectangle {x,y,width,height} before resizing.",
			"additionalProperties": false,
			"properties": map[string]any{
				"x": intProp("Crop left coordinate."), "y": intProp("Crop top coordinate."),
				"width":  map[string]any{"type": "integer", "description": "Crop width.", "minimum": 1},
				"height": map[string]any{"type": "integer", "description": "Crop height.", "minimum": 1},
			},
		}
		schema := toolcontract.InputObject(props)
		// 三种来源必须且只能选择一种。每个分支显式声明 object，兼容严格工具供应商。
		schema["oneOf"] = []map[string]any{
			{"type": "object", "required": []string{"artifact_id"}},
			{"type": "object", "required": []string{"path"}},
			{"type": "object", "required": []string{"url"}},
		}
		return schema, true
	case ToolFilePublish:
		props["file"] = map[string]any{"type": "string", "format": "binary", "description": "Top-level file parameter. Connector runtimes should pass the mounted local path when available."}
		props["path"] = stringProp("Local file or directory path visible to this AgentDock instance. Relative paths resolve from ~/AgentDock.")
		props["retention_seconds"] = boundedIntProp("Signed URL retention seconds. Zero uses the default 86400 and values are capped at 604800.", 0, int(publicartifacts.MaxRetention/time.Second))
	default:
		return nil, false
	}
	return toolcontract.InputObject(props, required...), true
}

func OutputSchema(name string) (map[string]any, bool) {
	stringProp := toolcontract.String
	intProp := toolcontract.Integer
	boolProp := toolcontract.Boolean
	objectProp := toolcontract.OpenObject
	props := map[string]any{}

	switch name {
	case ToolViewImage:
		props["source"] = objectProp("Resolved artifact, path, or URL source metadata.")
		props["image"] = objectProp("Processed image metadata attached as standard MCP image content.")
		props["original"] = objectProp("Original/crop metadata.")
		props["resized"] = boolProp("Whether image bytes changed due to crop/resize/re-encode.")
		props["warnings"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	case ToolFilePublish:
		props["artifact_id"] = stringProp("Published artifact id.")
		props["url"] = stringProp("Optional temporary signed download URL when a reachable base URL is available.")
		props["expires_at"] = stringProp("Signed URL expiry timestamp.")
		props["sha256"] = stringProp("Snapshot payload SHA-256.")
		props["size_bytes"] = intProp("Snapshot payload size in bytes.")
		props["mime_type"] = stringProp("Payload media type.")
		props["filename"] = stringProp("Download filename used for Content-Disposition and signature verification.")
		props["archive"] = boolProp("Whether the source directory was packaged as tar.gz.")
		props["width"] = intProp("Image width when the payload is an image.")
		props["height"] = intProp("Image height when the payload is an image.")
	default:
		return nil, false
	}
	return toolcontract.OutputObject(props), true
}
