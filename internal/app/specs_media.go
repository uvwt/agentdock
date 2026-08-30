package app

import (
	"context"

	toolmedia "github.com/uvwt/agentdock/internal/tool/media"
)

func imageToolSpecs() []ToolSpec {
	return []ToolSpec{{Name: "view_image", Title: "View image", Description: "Load an image by AgentDock artifact_id, Host path, or HTTP(S) URL and return it as standard MCP image content.", Annotations: readOnlyToolAnnotations(true), Handler: typedToolHandler("view_image", func(ctx context.Context, r *Runtime, request toolmedia.ViewImageRequest) (Result, error) {
		return r.media.ViewImage(ctx, request)
	})}}
}

func publishToolSpecs() []ToolSpec {
	return []ToolSpec{{Name: "file_publish", Title: "Publish signed file", Description: "Publish a local file or directory as an immutable Artifact snapshot under ~/.agentdock/public-artifacts. Returns artifact_id and, when a reachable base URL is available, a temporary signed download URL. Directories are packaged as tar.gz.", Annotations: mutatingToolAnnotations(false, true), FileArgRewritePaths: []string{"file"}, Handler: typedToolHandler("file_publish", func(ctx context.Context, r *Runtime, request toolmedia.FilePublishRequest) (Result, error) {
		return r.media.FilePublish(ctx, request)
	})}}
}
