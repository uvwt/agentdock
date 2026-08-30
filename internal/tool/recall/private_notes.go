package recall

import (
	"context"
	"net/http"
	"strings"
)

const (
	maxPrivateNoteSearchResults = 100
	maxPrivateNoteReadBytes     = 1 << 20
)

func (svc *Service) PrivateNoteManage(ctx context.Context, request PrivateNoteRequest) (Result, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	payload := map[string]any{}
	endpoint := ""

	switch action {
	case "search":
		endpoint = "/v1/private-notes/search"
		if request.Query != "" {
			payload["query"] = request.Query
		}
		if request.MaxResults != nil {
			payload["max_results"] = *request.MaxResults
		}
	case "read":
		endpoint = "/v1/private-notes/read"
		if request.Path != "" {
			payload["path"] = request.Path
		}
		if request.MaxBytes != nil {
			payload["max_bytes"] = *request.MaxBytes
		}
	case "write":
		endpoint = "/v1/private-notes/write"
		copyPrivateNoteStrings(payload, map[string]string{
			"path": request.Path, "category": request.Category, "title": request.Title,
			"summary": request.Summary, "content": request.Content,
		})
		if len(request.Tags) > 0 {
			payload["tags"] = append([]string(nil), request.Tags...)
		}
		if request.Confirmed != nil {
			payload["confirmed"] = *request.Confirmed
		}
		if request.Overwrite != nil {
			payload["overwrite"] = *request.Overwrite
		}
	case "delete":
		endpoint = "/v1/private-notes/delete"
		if request.Path != "" {
			payload["path"] = request.Path
		}
		if request.Confirmed != nil {
			payload["confirmed"] = *request.Confirmed
		}
	case "status":
		endpoint = "/v1/private-notes/status"
		statusAction := strings.ToLower(strings.TrimSpace(request.StatusAction))
		if statusAction == "" {
			statusAction = "check"
		}
		payload["action"] = statusAction
	case "maintain":
		endpoint = "/v1/private-notes/maintenance"
		maintenanceAction := strings.ToLower(strings.TrimSpace(request.MaintenanceAction))
		if maintenanceAction == "" {
			maintenanceAction = "sync-encrypted"
		}
		payload["action"] = maintenanceAction
	default:
		return nil, toolErrorDetails("INVALID_PRIVATE_NOTE_ACTION", "unsupported private_note_manage action", "validation", map[string]any{
			"action":  action,
			"allowed": []string{"search", "read", "write", "delete", "status", "maintain"},
		})
	}

	result, err := svc.request(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return nil, err
	}
	result["private_note_store"] = "NexusDock Private Notes"
	return result, nil
}

func copyPrivateNoteStrings(dst map[string]any, values map[string]string) {
	for key, value := range values {
		if value != "" {
			dst[key] = value
		}
	}
}
