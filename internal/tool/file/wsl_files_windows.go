//go:build windows

package file

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	pathpkg "path"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/workspace"
)

//go:embed wsl_file_helper.py
var wslFileHelper string

const maxWSLFileHelperOutputBytes = maxTextFileReadBytes + maxTextOutputBytes + (2 << 20)

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
	wslPath, err := exec.LookPath("wsl.exe")
	if err != nil {
		return nil, toolErrorDetails("WSL_NOT_AVAILABLE", "wsl.exe was not found on this Windows host", "runtime", map[string]any{"reason": err.Error()})
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode WSL file helper request: %w", err)
	}
	args := make([]string, 0, 8)
	if selection.Distribution != "" {
		args = append(args, "--distribution", selection.Distribution)
	}
	args = append(args, "--exec", "python3", "-c", wslFileHelper)

	commandEnv, err := svc.commandEnv("", nil)
	if err != nil {
		return nil, err
	}
	commandCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, wslPath, args...)
	cmd.Dir = svc.ws.DefaultCWD()
	cmd.Env = commandEnv
	cmd.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if commandCtx.Err() == context.DeadlineExceeded {
			return nil, toolErrorDetails(
				"WSL_FILE_TIMEOUT",
				"WSL file operation exceeded the 60 second timeout",
				"runtime",
				map[string]any{"wsl_distribution": selection.Distribution},
			)
		}
		if strings.Contains(strings.ToLower(message), "python3") {
			return nil, toolErrorDetails(
				"WSL_PYTHON_NOT_AVAILABLE",
				"runtime=wsl file tools require python3 in the selected distribution",
				"runtime",
				map[string]any{"wsl_distribution": selection.Distribution, "reason": message},
			)
		}
		return nil, toolErrorDetails(
			"WSL_FILE_RUNTIME_ERROR",
			"WSL file helper failed to start",
			"runtime",
			map[string]any{"wsl_distribution": selection.Distribution, "reason": err.Error(), "stderr": truncateString(message, 2000)},
		)
	}
	if stdout.Len() > maxWSLFileHelperOutputBytes {
		return nil, toolErrorDetails(
			"WSL_FILE_OUTPUT_TOO_LARGE",
			"WSL file helper output exceeded the safe limit",
			"runtime",
			map[string]any{"output_bytes": stdout.Len(), "max_output_bytes": maxWSLFileHelperOutputBytes},
		)
	}
	result := Result{}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, toolErrorDetails(
			"WSL_FILE_INVALID_RESPONSE",
			"WSL file helper returned invalid JSON",
			"runtime",
			map[string]any{"reason": err.Error(), "stderr": truncateString(strings.TrimSpace(stderr.String()), 2000)},
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

func (svc *Service) readFileWSL(ctx context.Context, args map[string]any, selection fileRuntimeSelection) (Result, error) {
	rawPath := stringArg(args, "path", "")
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
	maxBytes := boundedInt(intArg(args, "max_bytes", 262144), 262144, 1, maxTextOutputBytes)
	sliced, meta := sliceText(content, intArg(args, "start_line", 1), intArg(args, "end_line", 0), maxBytes)
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

func (svc *Service) listDirWSL(ctx context.Context, args map[string]any, selection fileRuntimeSelection) (Result, error) {
	path, err := resolveWSLFilePath(stringArg(args, "path", ""))
	if err != nil {
		return nil, err
	}
	return svc.callWSLFileHelper(ctx, selection, map[string]any{
		"action":          "list_dir",
		"path":            path,
		"recursive":       boolArg(args, "recursive", false),
		"max_depth":       boundedInt(intArg(args, "max_depth", 1), 1, 1, 20),
		"max_entries":     boundedInt(intArg(args, "max_entries", 200), 200, 1, 2000),
		"include_hidden":  boolArg(args, "include_hidden", false),
		"include_ignored": boolArg(args, "include_ignored", false),
	})
}

func (svc *Service) listFilesWSL(ctx context.Context, args map[string]any, selection fileRuntimeSelection) (Result, error) {
	path, err := resolveWSLFilePath(stringArg(args, "path", ""))
	if err != nil {
		return nil, err
	}
	patterns := stringSliceArg(args, "patterns")
	if len(patterns) == 0 {
		patterns = []string{"**/*"}
	}
	if glob := stringArg(args, "glob", ""); glob != "" {
		patterns = []string{glob}
	}
	return svc.callWSLFileHelper(ctx, selection, map[string]any{
		"action":           "list_files",
		"path":             path,
		"patterns":         patterns,
		"exclude_patterns": stringSliceArg(args, "exclude_patterns"),
		"max_results":      boundedInt(intArg(args, "max_results", 500), 500, 1, 5000),
		"include_hidden":   boolArg(args, "include_hidden", false),
		"include_ignored":  boolArg(args, "include_ignored", false),
	})
}

func (svc *Service) searchTextWSL(ctx context.Context, args map[string]any, selection fileRuntimeSelection) (Result, error) {
	query := stringArg(args, "query", "")
	if query == "" {
		return nil, toolError("INVALID_ARGUMENT", "query is required", "validation")
	}
	path, err := resolveWSLFilePath(stringArg(args, "path", ""))
	if err != nil {
		return nil, err
	}
	includeGlobs := stringSliceArg(args, "include_globs")
	if glob := stringArg(args, "glob", ""); glob != "" {
		includeGlobs = append(includeGlobs, glob)
	}
	return svc.callWSLFileHelper(ctx, selection, map[string]any{
		"action":          "search_text",
		"path":            path,
		"query":           query,
		"regex":           boolArg(args, "regex", false),
		"case_sensitive":  boolArg(args, "case_sensitive", false),
		"include_hidden":  boolArg(args, "include_hidden", false),
		"include_ignored": boolArg(args, "include_ignored", false),
		"include_globs":   includeGlobs,
		"exclude_globs":   stringSliceArg(args, "exclude_globs"),
		"context_lines":   boundedInt(intArg(args, "context_lines", 0), 0, 0, 20),
		"max_results":     boundedInt(intArg(args, "max_results", 100), 100, 1, 1000),
	})
}
