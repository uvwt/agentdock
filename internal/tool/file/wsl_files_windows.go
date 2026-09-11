//go:build windows

package file

import (
	"context"
	"encoding/json"
	"fmt"
	pathpkg "path"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/workspace"
)

const maxWSLFileHelperOutputBytes = maxTextFileReadBytes + maxTextOutputBytes + (2 << 20)

const maxWSLFileHelperInputBytes = 64 << 20

func wslFileErrorPhase(code string) string {
	switch code {
	case "INVALID_ARGUMENT",
		"PATH_NOT_FOUND",
		"IS_DIRECTORY",
		"NOT_A_DIRECTORY",
		"NOT_REGULAR_FILE",
		"FILE_TOO_LARGE",
		"BINARY_FILE",
		"ENCODING_UNSUPPORTED",
		"INVALID_REGEX",
		"PROTECTED_WSL_PATH",
		"SYMLINK_NOT_ALLOWED",
		"FILE_EXISTS",
		"OWNERSHIP_CHANGE_BLOCKED",
		"CROSS_DEVICE_MOVE":
		return "validation"
	default:
		return "runtime"
	}
}

func resolveWSLFilePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", toolError("INVALID_ARGUMENT", "an absolute WSL path is required", "validation")
	}
	if strings.ContainsRune(raw, 0) {
		return "", toolError("INVALID_ARGUMENT", "path contains an invalid byte", "validation")
	}
	if converted, ok := workspace.WindowsPathToWSL(raw); ok {
		return pathpkg.Clean(converted), nil
	}
	if !strings.HasPrefix(raw, "/") {
		return "", toolErrorDetails(
			"INVALID_ARGUMENT",
			"runtime=wsl requires an absolute Linux path or an absolute Windows drive path",
			"validation",
			map[string]any{"path": raw},
		)
	}
	return pathpkg.Clean(raw), nil
}

func (svc *Service) callWSLFileHelper(ctx context.Context, selection fileRuntimeSelection, request map[string]any) (Result, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode WSL file helper request: %w", err)
	}
	if len(payload) > maxWSLFileHelperInputBytes {
		return nil, toolErrorDetails(
			"WSL_FILE_INPUT_TOO_LARGE",
			"WSL file helper request exceeds the safe input limit",
			"validation",
			map[string]any{"input_bytes": len(payload), "max_input_bytes": maxWSLFileHelperInputBytes},
		)
	}
	helper, err := svc.ensureWSLFileHelper(ctx, selection)
	if err != nil {
		return nil, err
	}
	commandCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	commandResult, err := svc.runWSLCommand(commandCtx, helper.WSLExecutable, selection, payload, helper.WSLPath)
	if err != nil {
		message := strings.TrimSpace(string(commandResult.Stderr))
		if commandResult.TimedOut {
			return nil, toolErrorDetails(
				"WSL_FILE_TIMEOUT",
				"WSL file operation exceeded the 60 second timeout",
				"runtime",
				map[string]any{"wsl_distribution": selection.Distribution},
			)
		}
		// helper 可能在本进程运行期间被用户删除或 WSL 发行版被重置；清缓存后下一次调用会重新部署。
		invalidateWSLHelper(selection)
		return nil, toolErrorDetails(
			"WSL_FILE_RUNTIME_ERROR",
			"WSL file helper failed to start",
			"runtime",
			map[string]any{"wsl_distribution": selection.Distribution, "reason": err.Error(), "stderr": truncateString(message, 2000)},
		)
	}
	if len(commandResult.Stdout) > maxWSLFileHelperOutputBytes {
		return nil, toolErrorDetails(
			"WSL_FILE_OUTPUT_TOO_LARGE",
			"WSL file helper output exceeded the safe limit",
			"runtime",
			map[string]any{"output_bytes": len(commandResult.Stdout), "max_output_bytes": maxWSLFileHelperOutputBytes},
		)
	}
	result := Result{}
	if err := json.Unmarshal(commandResult.Stdout, &result); err != nil {
		return nil, toolErrorDetails(
			"WSL_FILE_INVALID_RESPONSE",
			"WSL file helper returned invalid JSON",
			"runtime",
			map[string]any{"reason": err.Error(), "stderr": truncateString(strings.TrimSpace(string(commandResult.Stderr)), 2000)},
		)
	}
	if ok, _ := result["ok"].(bool); !ok {
		code, _ := result["code"].(string)
		message, _ := result["message"].(string)
		details, _ := result["details"].(map[string]any)
		if details == nil {
			details = map[string]any{}
		}
		if selection.Distribution != "" {
			details["wsl_distribution"] = selection.Distribution
		}
		if code == "" {
			code = "WSL_FILE_RUNTIME_ERROR"
		}
		if message == "" {
			message = "WSL file helper failed"
		}
		return nil, toolErrorDetails(code, message, wslFileErrorPhase(code), details)
	}
	// ok 只属于 WSL 子进程内部协议；MCP 工具结果由 isError 表达调用错误。
	delete(result, "ok")
	delete(result, "code")
	delete(result, "message")
	delete(result, "details")
	return addFileRuntimeResult(result, selection), nil
}

func resultInt(result Result, key string) int {
	switch value := result[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func (svc *Service) readFileWSL(ctx context.Context, request ReadRequest, selection fileRuntimeSelection) (Result, error) {
	rawPath := request.Path
	if strings.HasPrefix(rawPath, "skill://") {
		return nil, toolError("INVALID_ARGUMENT", "skill:// resources use the Host runtime and cannot be read through WSL", "validation")
	}
	path, err := resolveWSLFilePath(rawPath)
	if err != nil {
		return nil, err
	}
	loaded, err := svc.callWSLFileHelper(ctx, selection, map[string]any{"action": "read", "path": path})
	if err != nil {
		return nil, err
	}
	content, _ := loaded["content"].(string)
	maxBytes := boundedInt(intValue(request.MaxBytes, 262144), 262144, 1, maxTextOutputBytes)
	sliced, meta := sliceText(content, intValue(request.StartLine, 1), intValue(request.EndLine, 0), maxBytes)
	result := Result{
		"path":        path,
		"content":     sliced,
		"encoding":    "utf-8",
		"size_bytes":  resultInt(loaded, "size_bytes"),
		"truncated":   meta.Truncated,
		"start_line":  meta.Start,
		"end_line":    meta.End,
		"total_lines": meta.Total,
	}
	if symlink, _ := loaded["symlink"].(bool); symlink {
		result["symlink"] = true
	}
	if meta.NextStartLine > 0 {
		result["next_start_line"] = meta.NextStartLine
	}
	if meta.TruncatedReason != "" {
		result["truncated_reason"] = meta.TruncatedReason
	}
	return addFileRuntimeResult(result, selection), nil
}

func (svc *Service) listDirWSL(ctx context.Context, request ListRequest, selection fileRuntimeSelection, opts listDirOptions) (Result, error) {
	path, err := resolveWSLFilePath(request.Path)
	if err != nil {
		return nil, err
	}
	return svc.callWSLFileHelper(ctx, selection, map[string]any{
		"action":           "list_dir",
		"path":             path,
		"max_depth":        opts.MaxDepth,
		"max_entries":      opts.MaxEntries,
		"patterns":         opts.Patterns,
		"exclude_patterns": opts.ExcludePatterns,
		"entry_type":       opts.EntryType,
		"include_hidden":   opts.IncludeHidden,
		"include_ignored":  opts.IncludeIgnored,
	})
}

func (svc *Service) searchTextWSL(ctx context.Context, request SearchRequest, selection fileRuntimeSelection) (Result, error) {
	if request.Query == "" {
		return nil, toolError("INVALID_ARGUMENT", "query is required", "validation")
	}
	path, err := resolveWSLFilePath(request.Path)
	if err != nil {
		return nil, err
	}
	includeGlobs := append([]string(nil), request.IncludeGlobs...)
	if request.Glob != "" {
		includeGlobs = append(includeGlobs, request.Glob)
	}
	return svc.callWSLFileHelper(ctx, selection, map[string]any{
		"action":          "search_text",
		"path":            path,
		"query":           request.Query,
		"regex":           request.Regex,
		"case_sensitive":  request.CaseSensitive,
		"include_hidden":  request.IncludeHidden,
		"include_ignored": request.IncludeIgnored,
		"include_globs":   includeGlobs,
		"exclude_globs":   append([]string(nil), request.ExcludeGlobs...),
		"context_lines":   boundedInt(intValue(request.ContextLines, 0), 0, 0, 20),
		"max_results":     boundedInt(intValue(request.MaxResults, 100), 100, 1, 1000),
	})
}
