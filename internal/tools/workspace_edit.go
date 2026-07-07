package tools

import (
	"context"
	"strings"
)

func (r *Runtime) workspaceEdit(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(stringArg(args, "action", ""))
	if action == "" {
		if stringArg(args, "patch", "") != "" {
			action = "patch"
		} else if stringArg(args, "old", "") != "" {
			action = "replace"
		}
	}
	switch action {
	case "patch", "apply_patch":
		result, err := r.applyPatch(ctx, args)
		if result != nil {
			result["action"] = "patch"
		}
		return result, err
	case "replace", "edit":
		result, err := r.editFile(args)
		if result != nil {
			result["action"] = "replace"
		}
		return result, err
	default:
		return nil, toolErrorDetails("INVALID_ACTION", "unsupported workspace_edit action", "validation", map[string]any{"action": action, "allowed": []string{"replace", "patch"}})
	}
}

func (r *Runtime) applyPatchCompat(ctx context.Context, args map[string]any) (Result, error) {
	nextArgs := copyArgs(args)
	nextArgs["action"] = "patch"
	result, err := r.workspaceEdit(ctx, nextArgs)
	return annotateDeprecated(result, "workspace_edit", nextArgs), err
}

func (r *Runtime) editFileCompat(args map[string]any) (Result, error) {
	nextArgs := copyArgs(args)
	nextArgs["action"] = "replace"
	result, err := r.workspaceEdit(context.Background(), nextArgs)
	return annotateDeprecated(result, "workspace_edit", nextArgs), err
}

func annotateDeprecated(result Result, replacementTool string, replacementArgs map[string]any) Result {
	if result == nil {
		return nil
	}
	result["deprecated"] = true
	result["replacement_tool"] = replacementTool
	result["replacement_args"] = replacementArgs
	return result
}
