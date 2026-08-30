package app

import (
	toolfile "github.com/uvwt/agentdock/internal/tool/file"
)

func fileOutputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "read_file":
		props["path"] = schemaStringProperty("Host path or skill:// resource URI. Relative Host paths resolve from ~/AgentDock.")
		props["content"] = schemaStringProperty("Text content slice.")
		props["encoding"] = schemaStringProperty("Detected text encoding.")
		props["size_bytes"] = schemaIntegerProperty("File size in bytes.")
		props["truncated"] = schemaBooleanProperty("Whether output was truncated.")
		props["truncated_reason"] = schemaStringProperty("Reason output was truncated.")
		props["start_line"] = schemaIntegerProperty("Returned start line.")
		props["end_line"] = schemaIntegerProperty("Returned end line.")
		props["next_start_line"] = schemaIntegerProperty("Next line to read when output was truncated.")
		props["total_lines"] = schemaIntegerProperty("Total line count.")
	case "list_dir":
		props["path"] = schemaStringProperty("Listed Host directory path. Relative paths resolve from ~/AgentDock.")
		props["entries"] = map[string]any{
			"type":        "array",
			"description": "Matched directory entries. Each entry path is slash-normalized and relative to the requested path.",
			"items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"name", "path", "type", "size_bytes", "modified", "is_hidden"},
				"properties": map[string]any{
					"name":       schemaStringProperty("Entry base name."),
					"path":       schemaStringProperty("Slash-normalized path relative to the requested directory."),
					"type":       map[string]any{"type": "string", "enum": []string{"file", "directory"}},
					"size_bytes": schemaIntegerProperty("Entry size in bytes as reported by the selected runtime."),
					"modified":   schemaStringProperty("Entry modification time in RFC3339-compatible form."),
					"is_hidden":  schemaBooleanProperty("Whether the entry path contains a hidden component."),
				},
			},
		}
		props["truncated"] = schemaBooleanProperty("Whether at least one additional matching entry existed beyond max_entries.")
		props["partial"] = schemaBooleanProperty("Whether unreadable descendant paths were skipped while returning readable entries.")
		props["skipped_paths"] = schemaArrayStringsProperty("Unreadable descendant paths skipped during traversal, relative to the requested path.")
		required = []string{"path", "entries", "truncated", "partial", "skipped_paths"}
	case "search_text":
		props["matches"] = schemaArrayObjectsProperty("Text search matches.")
		props["engine"] = schemaStringProperty("Search engine used: rg or go_fallback.")
		props["truncated"] = schemaBooleanProperty("Whether matches were truncated.")
	case "file_edit":
		props["action"] = schemaStringProperty("File edit action.")
		props["summary"] = schemaStringProperty("Result summary.")
		props["path"] = schemaStringProperty("Host path. Relative paths resolve from ~/AgentDock.")
		props["new_path"] = schemaStringProperty("Move destination path.")
		props["workdir"] = schemaStringProperty("Patch working directory.")
		props["affected_files"] = map[string]any{"type": "array", "description": "Files affected by a patch.", "items": map[string]any{"type": "string"}}
		props["dry_run"] = schemaBooleanProperty("Whether this was a dry run.")
		props["matches"] = schemaIntegerProperty("Match count for replace.")
		props["changed"] = schemaBooleanProperty("Whether content changed.")
		props["recursive"] = schemaBooleanProperty("Whether delete was allowed to remove a directory recursively.")
		props["diff_preview"] = schemaStringProperty("Diff preview.")
		props["truncated"] = schemaBooleanProperty("Whether the diff preview was truncated.")
		props["files_changed"] = schemaIntegerProperty("Changed file count.")
		props["insertions"] = schemaIntegerProperty("Inserted line count.")
		props["deletions"] = schemaIntegerProperty("Deleted line count.")
	}
	toolfile.AddRuntimeOutputProperties(props)
	return finalizeOutputSchema(props, required)
}
