package tools

import (
	"context"
	"net/http"
	"strings"
)

const (
	maxPrivateNoteSearchResults = 100
	maxPrivateNoteReadBytes     = 1 << 20
)

func (r *Runtime) privateNoteManage(ctx context.Context, args map[string]any) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(stringArg(args, "action", "")))
	payload := map[string]any{}
	endpoint := ""

	switch action {
	case "search":
		endpoint = "/v1/private-notes/search"
		copyPrivateNoteArgs(args, payload, "query", "max_results")
	case "read":
		endpoint = "/v1/private-notes/read"
		copyPrivateNoteArgs(args, payload, "path", "max_bytes")
	case "write":
		endpoint = "/v1/private-notes/write"
		copyPrivateNoteArgs(args, payload, "path", "category", "title", "summary", "tags", "content", "confirmed", "overwrite")
	case "delete":
		endpoint = "/v1/private-notes/delete"
		copyPrivateNoteArgs(args, payload, "path", "confirmed")
	case "status":
		endpoint = "/v1/private-notes/status"
		payload["action"] = strings.ToLower(strings.TrimSpace(stringArg(args, "status_action", "check")))
	case "maintain":
		endpoint = "/v1/private-notes/maintenance"
		payload["action"] = strings.ToLower(strings.TrimSpace(stringArg(args, "maintenance_action", "sync-encrypted")))
	default:
		return nil, toolErrorDetails("INVALID_PRIVATE_NOTE_ACTION", "unsupported private_note_manage action", "validation", map[string]any{
			"action":  action,
			"allowed": []string{"search", "read", "write", "delete", "status", "maintain"},
		})
	}

	result, err := r.memoryRequest(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return nil, err
	}
	result["private_note_store"] = "NexusDock Private Notes"
	return result, nil
}

func copyPrivateNoteArgs(src, dst map[string]any, keys ...string) {
	for _, key := range keys {
		value, ok := src[key]
		if !ok || value == nil {
			continue
		}
		dst[key] = value
	}
}
