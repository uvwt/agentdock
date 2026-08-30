package browser

import (
	"errors"
	"testing"
)

func requireBrowserErrorCode(t *testing.T, err error, code string) *Error {
	t.Helper()
	var browserErr *Error
	if !errors.As(err, &browserErr) {
		t.Fatalf("error = %T %v, want *browser.Error", err, err)
	}
	if browserErr.Code != code {
		t.Fatalf("error code = %q, want %q", browserErr.Code, code)
	}
	return browserErr
}

func TestParseBrowserActionRejectsLegacyAndUnknownFields(t *testing.T) {
	tests := []map[string]any{
		{"action": "click", "selector": "#ok", "backend": "cdp"},
		{"action": "wait_for_text", "value": "legacy"},
		{"action": "goto", "url": "https://example.test", "wait_until": "networkidle"},
		{"action": "click", "selector": "text=Save"},
		{"action": "click", "selector": "//button"},
		{"action": "wait_for_response", "status": 200},
		{"action": "wait_for_response", "url": "/api", "status": 0},
		{"action": "scroll", "delta_x": 0, "delta_y": 0},
		{"action": "unknown"},
	}
	for index, input := range tests {
		if _, err := parseBrowserAction(input); err == nil {
			t.Fatalf("case %d parseBrowserAction(%#v) unexpectedly succeeded", index, input)
		} else {
			requireBrowserErrorCode(t, err, ErrActionInvalid)
		}
	}
}

func TestParseBrowserActionsAddsActionIndexToValidationError(t *testing.T) {
	_, err := parseBrowserActions([]any{
		map[string]any{"action": "click", "selector": "#ok"},
		map[string]any{"action": "wait_for_text", "text": "ready", "bogus": true},
	})
	browserErr := requireBrowserErrorCode(t, err, ErrActionInvalid)
	if browserErr.Details == nil || browserErr.Details.ActionIndex == nil || *browserErr.Details.ActionIndex != 1 {
		t.Fatalf("action_index details = %#v, want 1", browserErr.Details)
	}
}

func TestParseBrowserStartUsesNativeContract(t *testing.T) {
	req, err := parseBrowserStart(map[string]any{
		"url":        "https://example.test",
		"browser":    "edge",
		"headless":   false,
		"viewport":   map[string]any{"width": 1440.0, "height": 900.0},
		"profile_id": "work",
		"cookies": []any{map[string]any{
			"name": "sid", "value": "abc", "url": "https://example.test", "same_site": "lax",
		}},
		"local_storage": map[string]any{"https://example.test": map[string]any{"theme": "dark"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Browser != BrowserEdge || req.Viewport.Width != 1440 || req.Viewport.Height != 900 || req.ProfileID != "work" || req.Headless {
		t.Fatalf("request = %#v", req)
	}
	if len(req.Cookies) != 1 || req.Cookies[0].Name != "sid" || req.LocalStorage["https://example.test"]["theme"] != "dark" {
		t.Fatalf("injected state = %#v %#v", req.Cookies, req.LocalStorage)
	}
}

func TestBrowserFailureNeverExposesProcessDiagnostics(t *testing.T) {
	result := browserFailure(&Error{
		Code: ErrLaunchFailed, Message: "failed", Phase: "browser_launch",
		Details: &ErrorDetails{Path: "/missing/chrome"},
	})
	if result["browser_ok"] != false || result["code"] != "LAUNCH_FAILED" {
		t.Fatalf("result = %#v", result)
	}
	for _, forbidden := range []string{"stdout", "stderr", "suggested_retry", "backend", "cdp_url"} {
		if _, exists := result[forbidden]; exists {
			t.Fatalf("browser failure exposed %s: %#v", forbidden, result)
		}
	}
}
