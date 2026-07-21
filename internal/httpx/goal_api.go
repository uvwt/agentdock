package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
	"github.com/uvwt/agentdock/internal/tools"
)

// registerGoalAPI exposes JSON-only Goal / ChatGPT worker / orchestrator endpoints for
// automation and CLI-style clients. There is intentionally no HTML operator dashboard:
// the product UX is ChatGPT web inside the dedicated Chromium profile.
func registerGoalAPI(mux *http.ServeMux, server *mcp.Server, cfg config.Config, oauthStore *auth.OAuthStore) {
	api := goalAPIHandler(server, cfg, oauthStore)
	mux.HandleFunc("/internal/runtime/goals", api)
	mux.HandleFunc("/internal/runtime/goals/", api)
	mux.HandleFunc("/internal/runtime/chatgpt/worker", chatgptWorkerAPIHandler(server, cfg, oauthStore))
}

func chatgptWorkerAPIHandler(server *mcp.Server, cfg config.Config, oauthStore *auth.OAuthStore) http.HandlerFunc {
	authorizer := auth.Bearer{Token: cfg.AuthToken}
	authRequired := cfg.AuthRequired()
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeOperator(r, cfg, oauthStore, authorizer, authRequired) {
			setBearerChallenge(w, cfg, r, strings.TrimSpace(r.Header.Get("Authorization")) != "")
			writeRuntimeAPIError(w, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, server.RuntimeChatGPTWorkerStatus())
		case http.MethodPost:
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			var payload struct {
				AutoWake          *bool  `json:"auto_wake"`
				AutoApproveTools  *bool  `json:"auto_approve_tools"`
				Action            string `json:"action"`
			}
			_ = json.Unmarshal(body, &payload)
			// Apply both flags when present. Previously auto_wake short-circuited
			// and left auto_approve_tools unchanged (r5 setup bug).
			applied := false
			if payload.AutoWake != nil {
				_ = server.RuntimeSetChatGPTAutoWake(*payload.AutoWake)
				applied = true
			}
			if payload.AutoApproveTools != nil {
				_ = server.RuntimeSetChatGPTAutoApproveTools(*payload.AutoApproveTools)
				applied = true
			}
			if applied {
				writeJSON(w, server.RuntimeChatGPTWorkerStatus())
				return
			}
			if strings.EqualFold(payload.Action, "open") {
				result, err := server.RuntimeChatGPTOpen(r.Context())
				if err != nil {
					writeRuntimeAPIHandlerError(w, err)
					return
				}
				writeJSON(w, result)
				return
			}
			if strings.EqualFold(payload.Action, "force_rotate") || strings.EqualFold(payload.Action, "new_session") {
				writeJSON(w, server.RuntimeChatGPTForceRotate())
				return
			}
			writeJSON(w, server.RuntimeChatGPTWorkerStatus())
		default:
			writeRuntimeAPIError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		}
	}
}

func goalAPIHandler(server *mcp.Server, cfg config.Config, oauthStore *auth.OAuthStore) http.HandlerFunc {
	authorizer := auth.Bearer{Token: cfg.AuthToken}
	authRequired := cfg.AuthRequired()
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeOperator(r, cfg, oauthStore, authorizer, authRequired) {
			setBearerChallenge(w, cfg, r, strings.TrimSpace(r.Header.Get("Authorization")) != "")
			writeRuntimeAPIError(w, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
			return
		}
		path := strings.TrimSuffix(r.URL.Path, "/")
		switch {
		case path == "/internal/runtime/goals" && r.Method == http.MethodGet:
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			result, err := server.RuntimeGoals(r.URL.Query().Get("status"), limit)
			if err != nil {
				writeRuntimeAPIHandlerError(w, err)
				return
			}
			writeJSON(w, result)
		case path == "/internal/runtime/goals/unbind" && r.Method == http.MethodPost:
			writeJSON(w, server.RuntimeGoalUnbind())
		case strings.HasPrefix(path, "/internal/runtime/goals/"):
			rest := strings.TrimPrefix(path, "/internal/runtime/goals/")
			parts := strings.Split(rest, "/")
			if len(parts) == 0 || parts[0] == "" {
				writeRuntimeAPIError(w, http.StatusNotFound, "NOT_FOUND", "goal id required")
				return
			}
			goalID := parts[0]
			if len(parts) == 1 && r.Method == http.MethodGet {
				result, err := server.RuntimeGoal(goalID)
				if err != nil {
					writeRuntimeAPIHandlerError(w, err)
					return
				}
				writeJSON(w, result)
				return
			}
			if len(parts) == 2 && r.Method == http.MethodPost {
				action := parts[1]
				summary := readJSONField(r, "summary")
				var (
					result tools.Result
					err    error
				)
				switch action {
				case "pause":
					result, err = server.RuntimeGoalPause(goalID, summary)
				case "resume":
					result, err = server.RuntimeGoalResume(goalID, summary)
				case "cancel":
					result, err = server.RuntimeGoalCancel(goalID, summary)
				case "bind":
					result, err = server.RuntimeGoalBind(goalID)
				case "chatgpt_wake":
					result, err = server.RuntimeChatGPTWake(r.Context(), goalID)
				case "request_reasoning":
					result, err = server.RuntimeRequestReasoning(goalID, summary, readJSONField(r, "problem"))
				case "orchestrate_start":
					result, err = server.RuntimeOrchestratorStart(goalID)
				case "orchestrate_stop":
					result = server.RuntimeOrchestratorStop(goalID)
				case "orchestrate_status":
					result = server.RuntimeOrchestratorStatus(goalID)
				default:
					writeRuntimeAPIError(w, http.StatusNotFound, "NOT_FOUND", "unknown goal action")
					return
				}
				if err != nil {
					writeRuntimeAPIHandlerError(w, err)
					return
				}
				writeJSON(w, result)
				return
			}
			if len(parts) == 3 && parts[1] == "approvals" && r.Method == http.MethodPost {
				decision, note := readApprovalBody(r)
				result, err := server.RuntimeResolveGoalApproval(goalID, parts[2], decision, note)
				if err != nil {
					writeRuntimeAPIHandlerError(w, err)
					return
				}
				writeJSON(w, result)
				return
			}
			writeRuntimeAPIError(w, http.StatusNotFound, "NOT_FOUND", "goal API route not found")
		default:
			writeRuntimeAPIError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		}
	}
}

func authorizeOperator(r *http.Request, cfg config.Config, oauthStore *auth.OAuthStore, authorizer auth.Bearer, authRequired bool) bool {
	staticOK := cfg.AuthToken != "" && authorizer.Authorized(r)
	oauthOK := authorizedOAuth(r, cfg, oauthStore)
	if authRequired && !staticOK && !oauthOK {
		if tok := r.URL.Query().Get("token"); tok != "" && tok == cfg.AuthToken {
			return true
		}
		return false
	}
	return true
}

// readJSONField returns one JSON object field. The request body is buffered and
// restored so multiple fields can be read from the same POST (r9: problem was
// always empty because summary consumed r.Body first).
func readJSONField(r *http.Request, field string) string {
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		var payload map[string]any
		if json.Unmarshal(body, &payload) == nil {
			if v, ok := payload[field]; ok {
				return strings.TrimSpace(stringifyAny(v))
			}
		}
		return ""
	}
	_ = r.ParseForm()
	return strings.TrimSpace(r.FormValue(field))
}

func readApprovalBody(r *http.Request) (decision, note string) {
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var payload struct {
			Decision string `json:"decision"`
			Note     string `json:"note"`
		}
		_ = json.Unmarshal(body, &payload)
		return payload.Decision, payload.Note
	}
	_ = r.ParseForm()
	return r.FormValue("decision"), r.FormValue("note")
}

func stringifyAny(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
