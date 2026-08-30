package app

import (
	toolfile "github.com/uvwt/agentdock/internal/tool/file"
)

func fileInputSchema(name string) map[string]any {
	props := map[string]any{}
	required := []string{}
	switch name {
	case "read_file":
		props["path"] = schemaStringProperty(toolfile.PathDescription("Host path. Relative paths resolve from ~/AgentDock."))
		toolfile.AddRuntimeProperties(props)
		props["start_line"] = schemaIntegerProperty("1-based start line.")
		props["end_line"] = schemaIntegerProperty("Inclusive end line.")
		props["max_bytes"] = schemaBoundedIntegerProperty("Maximum output bytes. Defaults to 262144 and is capped at 4194304.", 1, toolfile.MaxTextOutputBytes)
		required = []string{"path"}
	case "list_dir":
		props["path"] = schemaStringProperty(toolfile.PathDescription("Host directory path. Relative paths resolve from ~/AgentDock."))
		toolfile.AddRuntimeProperties(props)
		props["max_depth"] = schemaBoundedIntegerProperty("Maximum traversal depth relative to path. Defaults to 1 and is capped at 20.", 1, 20)
		props["max_entries"] = schemaBoundedIntegerProperty("Maximum returned entries. Defaults to 200 and is capped at 5000.", 1, 5000)
		props["patterns"] = map[string]any{"type": "array", "description": "Include glob patterns relative to path. * stays within one path segment; ** crosses directories. Defaults to [\"**/*\"].", "items": map[string]any{"type": "string"}}
		props["exclude_patterns"] = map[string]any{"type": "array", "description": "Exclude glob patterns relative to path, using the same * and ** semantics as patterns.", "items": map[string]any{"type": "string"}}
		props["entry_type"] = map[string]any{"type": "string", "description": "Return any entries, files only, or directories only. Defaults to any.", "enum": []string{"any", "file", "directory"}}
		props["include_hidden"] = schemaBooleanProperty("Include hidden paths.")
		props["include_ignored"] = schemaBooleanProperty("Include normally skipped or ignored paths.")
	case "search_text":
		props["path"] = schemaStringProperty(toolfile.PathDescription("Host path. Relative paths resolve from ~/AgentDock."))
		toolfile.AddRuntimeProperties(props)
		props["query"] = schemaStringProperty("Text or regex query.")
		props["regex"] = schemaBooleanProperty("Treat query as regex.")
		props["case_sensitive"] = schemaBooleanProperty("Use case-sensitive search.")
		props["include_hidden"] = schemaBooleanProperty("Include hidden files and directories.")
		props["include_globs"] = map[string]any{"type": "array", "description": "Include glob patterns relative to path. * stays within one path segment; ** crosses directories.", "items": map[string]any{"type": "string"}}
		props["glob"] = schemaStringProperty("Single include glob relative to path. * stays within one path segment; ** crosses directories.")
		props["exclude_globs"] = map[string]any{"type": "array", "description": "Exclude glob patterns relative to path, using the same * and ** semantics as include_globs.", "items": map[string]any{"type": "string"}}
		props["context_lines"] = schemaBoundedIntegerProperty("Context lines around each match. Capped at 20.", 0, 20)
		props["max_results"] = schemaBoundedIntegerProperty("Maximum matches. Defaults to 100 and is capped at 1000.", 1, 1000)
		required = []string{"query"}
	case "file_edit":
		props["action"] = map[string]any{"type": "string", "description": "File edit action.", "enum": []string{"replace", "patch", "add", "delete", "move"}}
		props["path"] = schemaStringProperty(toolfile.PathDescription("Host path for replace, add, delete, or move. Relative paths resolve from ~/AgentDock."))
		toolfile.AddRuntimeProperties(props)
		props["old"] = schemaStringProperty("Exact UTF-8 text to replace.")
		props["new"] = schemaStringProperty("Replacement UTF-8 text for action=replace.")
		props["replace_all"] = schemaBooleanProperty("Replace every match instead of only the first.")
		props["expected_matches"] = map[string]any{"type": "integer", "description": "Required number of matches. Defaults to 1; zero asserts no matches.", "minimum": 0}
		props["content"] = schemaStringProperty("Text content for action=add.")
		props["new_path"] = schemaStringProperty(toolfile.PathDescription("Destination path for action=move."))
		props["overwrite"] = schemaBooleanProperty("Allow add or move to replace an existing destination file.")
		props["recursive"] = schemaBooleanProperty("Required for deleting directories.")
		props["patch"] = schemaStringProperty(toolfile.PatchDescription("Patch text for action=patch."))
		props["workdir"] = schemaStringProperty(toolfile.PathDescription("Patch working directory."))
		props["dry_run"] = schemaBooleanProperty("Preview or validate without writing.")
		props["max_diff_bytes"] = schemaBoundedIntegerProperty("Maximum diff preview bytes. Defaults to 65536 and is capped at 4194304.", 1, toolfile.MaxTextOutputBytes)
		required = []string{"action"}
	}
	return finalizeInputSchema(name, props, required)
}
