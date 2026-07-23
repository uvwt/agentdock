package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Result map[string]any

type ToolError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Category  string         `json:"category"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
	cause     error
}

func (e *ToolError) Error() string { return e.Message }
func (e *ToolError) Unwrap() error { return e.cause }

func NewError(code, message, category string) *ToolError {
	return &ToolError{Code: code, Message: message, Category: category, Details: map[string]any{}}
}

func NewErrorDetails(code, message, category string, details map[string]any) *ToolError {
	return &ToolError{Code: code, Message: message, Category: category, Details: details}
}

func NewErrorCause(code, message, category string, details map[string]any, cause error) *ToolError {
	return &ToolError{Code: code, Message: message, Category: category, Details: details, cause: cause}
}

func StringArg(args map[string]any, key, fallback string) string {
	if value, ok := args[key]; ok && value != nil {
		return fmt.Sprint(value)
	}
	return fallback
}

func BoundedInt(value, fallback, minimum, maximum int) int {
	if value < minimum {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func BoundedMilliseconds(value, fallback, maximum int) time.Duration {
	milliseconds := BoundedInt(value, fallback, 1, maximum)
	return time.Duration(milliseconds) * time.Millisecond
}

func IntArg(args map[string]any, key string, fallback int) int {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed {
			return fallback
		}
		if parsed, err := strconv.ParseInt(strconv.FormatFloat(typed, 'f', -1, 64), 10, 0); err == nil {
			return int(parsed)
		}
	case json.Number:
		if parsed, err := strconv.ParseInt(string(typed), 10, 0); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return fallback
}

func BoolArg(args map[string]any, key string, fallback bool) bool {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		if parsed, err := strconv.ParseBool(typed); err == nil {
			return parsed
		}
	}
	return fallback
}

func StringSliceArg(args map[string]any, key string) []string {
	value, ok := args[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		return out
	case string:
		if typed != "" {
			return []string{typed}
		}
	}
	return nil
}

func MapArg(args map[string]any, key string) map[string]any {
	value, ok := args[key]
	if !ok || value == nil {
		return nil
	}
	result, _ := value.(map[string]any)
	return result
}

type BoundedOutput struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	total     int64
	truncated bool
}

func NewBoundedOutput(limit int) *BoundedOutput {
	return &BoundedOutput{limit: limit}
}

func (w *BoundedOutput) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	original := len(data)
	w.total += int64(original)
	remaining := w.limit - w.buffer.Len()
	if remaining <= 0 {
		w.truncated = w.truncated || original > 0
		return original, nil
	}
	if len(data) > remaining {
		data = data[:remaining]
		w.truncated = true
	}
	_, _ = w.buffer.Write(data)
	return original, nil
}

func (w *BoundedOutput) Snapshot() ([]byte, int64, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buffer.Bytes()...), w.total, w.truncated
}

func RunBoundedCombinedOutput(cmd *exec.Cmd, limit int) ([]byte, int64, bool, error) {
	output := NewBoundedOutput(limit)
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	data, total, truncated := output.Snapshot()
	return data, total, truncated, err
}

func ReadBoundedBody(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("response body limit must be positive")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxBytes)
	}
	return data, nil
}

var defaultSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]+`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]+`),
	regexp.MustCompile(`(?i)(password|token|secret)=([^\s&]+)`),
	regexp.MustCompile(`https://[^\s/@:]+:[^\s/@]+@github\.com`),
}

func RedactSecrets(value string, extraPatterns []string) string {
	out := value
	for _, expression := range defaultSecretPatterns {
		out = expression.ReplaceAllStringFunc(out, func(match string) string {
			if strings.HasPrefix(strings.ToLower(match), "https://") {
				return "https://***@github.com"
			}
			if index := strings.Index(match, "="); index >= 0 {
				return match[:index+1] + "***"
			}
			return "***"
		})
	}
	for _, pattern := range extraPatterns {
		if pattern == "" {
			continue
		}
		expression, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		out = expression.ReplaceAllString(out, "***")
	}
	return out
}
