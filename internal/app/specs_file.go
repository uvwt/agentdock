package app

import (
	"context"

	toolfile "github.com/uvwt/agentdock/internal/tool/file"
)

func fileToolSpecs() []ToolSpec {
	return []ToolSpec{
		{Name: "read_file", Title: "Read file", Description: toolfile.ToolDescription("Read a UTF-8 text file slice. Supports normal Host paths and skill://<name>/<path> resources from the active Skill version."), Annotations: readOnlyToolAnnotations(false), Handler: typedToolHandler("read_file", func(ctx context.Context, r *Runtime, request toolfile.ReadRequest) (Result, error) {
			return r.files.ReadFile(ctx, request)
		})},
		{Name: "list_dir", Title: "List directory", Description: toolfile.ToolDescription("List directory entries with explicit depth, glob filters, and entry-type filtering. Glob patterns are relative to path: * stays within one path segment and ** crosses directories. Relative paths resolve from ~/AgentDock; absolute and ~/ paths use Host rules."), Annotations: readOnlyToolAnnotations(false), Handler: typedToolHandler("list_dir", func(ctx context.Context, r *Runtime, request toolfile.ListRequest) (Result, error) {
			return r.files.ListDir(ctx, request)
		})},
		{Name: "search_text", Title: "Search text", Description: toolfile.ToolDescription("Search UTF-8 files for text or regex matches. Relative paths search ~/AgentDock by default; absolute paths are allowed."), Annotations: readOnlyToolAnnotations(false), Handler: typedToolHandler("search_text", func(ctx context.Context, r *Runtime, request toolfile.SearchRequest) (Result, error) {
			return r.files.SearchText(ctx, request)
		})},
		{Name: "file_edit", Title: "Edit file", Description: toolfile.EditDescription("Edit files through one action-based entrypoint: replace, patch, add, delete, or move. Relative paths resolve from ~/AgentDock; absolute and ~/ paths use Host rules."), Annotations: mutatingToolAnnotations(true, false), Handler: typedToolHandler("file_edit", func(ctx context.Context, r *Runtime, request toolfile.EditRequest) (Result, error) {
			return r.files.Edit(ctx, request)
		})},
	}
}
