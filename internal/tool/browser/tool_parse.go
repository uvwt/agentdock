package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

func parseBrowserStart(args map[string]any) (StartRequest, error) {
	urlValue, err := optionalString(args, "url", "about:blank")
	if err != nil {
		return StartRequest{}, err
	}
	browserValue, err := optionalString(args, "browser", string(BrowserAuto))
	if err != nil {
		return StartRequest{}, err
	}
	kind := Kind(strings.ToLower(browserValue))
	switch kind {
	case BrowserAuto, BrowserChrome, BrowserChromium, BrowserEdge:
	default:
		return StartRequest{}, browserInvalid("browser must be auto, chrome, chromium, or edge", map[string]any{"browser": browserValue})
	}
	headless, err := boolArgStrict(args, "headless", true)
	if err != nil {
		return StartRequest{}, err
	}
	viewport, err := parseViewport(args["viewport"])
	if err != nil {
		return StartRequest{}, err
	}
	profileID, err := optionalString(args, "profile_id", "")
	if err != nil {
		return StartRequest{}, err
	}
	cdpURL, err := optionalString(args, "cdp_url", "")
	if err != nil {
		return StartRequest{}, err
	}
	cookies, err := parseCookies(args["cookies"])
	if err != nil {
		return StartRequest{}, err
	}
	storage, err := parseLocalStorage(args["local_storage"])
	if err != nil {
		return StartRequest{}, err
	}
	reload, err := boolArgStrict(args, "reload_after_local_storage", true)
	if err != nil {
		return StartRequest{}, err
	}
	timeout, err := durationArg(args, "timeout_ms", 30*time.Second, time.Millisecond, 5*time.Minute)
	if err != nil {
		return StartRequest{}, err
	}
	return StartRequest{
		URL: urlValue, Browser: kind, Headless: headless, Viewport: viewport,
		ProfileID: profileID, CDPURL: cdpURL,
		Cookies: cookies, LocalStorage: storage,
		ReloadAfterLocalStorage: reload, Timeout: timeout,
	}, nil
}

func parseViewport(raw any) (Viewport, error) {
	if raw == nil {
		return Viewport{Width: 1280, Height: 800}, nil
	}
	value, ok := raw.(map[string]any)
	if !ok {
		return Viewport{}, browserInvalid("viewport must be an object", nil)
	}
	if err := validateBrowserKeys(value, "width", "height"); err != nil {
		return Viewport{}, err
	}
	width, err := intArgRange(value, "width", 1280, 320, 7680)
	if err != nil {
		return Viewport{}, err
	}
	height, err := intArgRange(value, "height", 800, 200, 4320)
	if err != nil {
		return Viewport{}, err
	}
	return Viewport{Width: width, Height: height}, nil
}

func parseCookies(raw any) ([]Cookie, error) {
	if raw == nil {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, browserInvalid("cookies must be an array", nil)
	}
	cookies := make([]Cookie, 0, len(values))
	for index, rawCookie := range values {
		value, ok := rawCookie.(map[string]any)
		if !ok {
			return nil, browserInvalid("each cookie must be an object", map[string]any{"index": index})
		}
		if err := validateBrowserKeys(value, "name", "value", "url", "domain", "path", "expires", "http_only", "secure", "same_site"); err != nil {
			return nil, err
		}
		name, err := requiredString(value, "name")
		if err != nil {
			return nil, err
		}
		cookieValue, err := requiredStringAllowEmpty(value, "value")
		if err != nil {
			return nil, err
		}
		urlValue, err := optionalString(value, "url", "")
		if err != nil {
			return nil, err
		}
		domain, err := optionalString(value, "domain", "")
		if err != nil {
			return nil, err
		}
		if urlValue == "" && domain == "" {
			return nil, browserInvalid("cookie requires url or domain", map[string]any{"index": index})
		}
		pathValue, err := optionalString(value, "path", "")
		if err != nil {
			return nil, err
		}
		expires, err := floatArg(value, "expires", 0)
		if err != nil || expires < 0 {
			return nil, browserInvalid("cookie expires must be a non-negative number", map[string]any{"index": index})
		}
		httpOnly, err := boolArgStrict(value, "http_only", false)
		if err != nil {
			return nil, err
		}
		secure, err := boolArgStrict(value, "secure", false)
		if err != nil {
			return nil, err
		}
		sameSite, err := optionalString(value, "same_site", "")
		if err != nil {
			return nil, err
		}
		if sameSite != "" {
			sameSite = strings.ToLower(sameSite)
			if sameSite != "strict" && sameSite != "lax" && sameSite != "none" {
				return nil, browserInvalid("cookie same_site must be strict, lax, or none", map[string]any{"index": index})
			}
		}
		cookies = append(cookies, Cookie{Name: name, Value: cookieValue, URL: urlValue, Domain: domain, Path: pathValue, Expires: expires, HTTPOnly: httpOnly, Secure: secure, SameSite: sameSite})
	}
	return cookies, nil
}

func parseLocalStorage(raw any) (map[string]map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	origins, ok := raw.(map[string]any)
	if !ok {
		return nil, browserInvalid("local_storage must be an object keyed by origin", nil)
	}
	result := make(map[string]map[string]string, len(origins))
	for origin, rawEntries := range origins {
		entries, ok := rawEntries.(map[string]any)
		if !ok || strings.TrimSpace(origin) == "" {
			return nil, browserInvalid("local_storage origins must contain string maps", map[string]any{"origin": origin})
		}
		typed := make(map[string]string, len(entries))
		for key, rawValue := range entries {
			value, ok := rawValue.(string)
			if !ok {
				return nil, browserInvalid("local_storage values must be strings", map[string]any{"origin": origin, "key": key})
			}
			typed[key] = value
		}
		result[origin] = typed
	}
	return result, nil
}

func parseBrowserActions(raw any) ([]Action, error) {
	values, ok := raw.([]any)
	if !ok || len(values) == 0 {
		return nil, browserInvalid("actions must be a non-empty array", nil)
	}
	if len(values) > 100 {
		return nil, browserInvalid("actions cannot contain more than 100 items", map[string]any{"count": len(values)})
	}
	actions := make([]Action, 0, len(values))
	for index, rawAction := range values {
		value, ok := rawAction.(map[string]any)
		if !ok {
			return nil, browserInvalid("each action must be an object", map[string]any{"action_index": index})
		}
		action, err := parseBrowserAction(value)
		if err != nil {
			var browserErr *Error
			if errors.As(err, &browserErr) {
				if browserErr.Details == nil {
					browserErr.Details = &ErrorDetails{}
				}
				browserErr.Details.ActionIndex = browserIntPointer(index)
			}
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, nil
}

func parseBrowserAction(value map[string]any) (Action, error) {
	name, err := requiredString(value, "action")
	if err != nil {
		return Action{}, err
	}
	timeout := func() (time.Duration, error) {
		return durationArg(value, "timeout_ms", 10*time.Second, time.Millisecond, 5*time.Minute)
	}
	waitUntil := func() (WaitUntil, error) {
		raw, err := optionalString(value, "wait_until", string(WaitLoad))
		if err != nil {
			return "", err
		}
		parsed := WaitUntil(strings.ToLower(raw))
		if parsed != WaitLoad && parsed != WaitDOMContentLoaded {
			return "", browserInvalid("wait_until must be domcontentloaded or load", map[string]any{"wait_until": raw})
		}
		return parsed, nil
	}
	state := func() (ElementState, error) {
		raw, err := optionalString(value, "state", string(StateVisible))
		if err != nil {
			return "", err
		}
		parsed := ElementState(strings.ToLower(raw))
		switch parsed {
		case StateVisible, StateHidden, StateAttached, StateDetached:
			return parsed, nil
		default:
			return "", browserInvalid("state must be visible, hidden, attached, or detached", map[string]any{"state": raw})
		}
	}

	switch name {
	case "goto":
		if err := validateBrowserKeys(value, "action", "url", "wait_until", "timeout_ms"); err != nil {
			return Action{}, err
		}
		urlValue, err := requiredString(value, "url")
		if err != nil {
			return Action{}, err
		}
		wait, err := waitUntil()
		if err != nil {
			return Action{}, err
		}
		t, err := timeout()
		if err != nil {
			return Action{}, err
		}
		return Action{Kind: name, Goto: &GotoAction{URL: urlValue, WaitUntil: wait, Timeout: t}}, nil
	case "click":
		if err := validateBrowserKeys(value, "action", "selector"); err != nil {
			return Action{}, err
		}
		selector, err := requiredCSSSelector(value)
		if err != nil {
			return Action{}, err
		}
		return Action{Kind: name, Click: &ClickAction{Selector: selector}}, nil
	case "fill":
		if err := validateBrowserKeys(value, "action", "selector", "value"); err != nil {
			return Action{}, err
		}
		selector, err := requiredCSSSelector(value)
		if err != nil {
			return Action{}, err
		}
		text, err := requiredStringAllowEmpty(value, "value")
		if err != nil {
			return Action{}, err
		}
		return Action{Kind: name, Fill: &FillAction{Selector: selector, Value: text}}, nil
	case "press":
		if err := validateBrowserKeys(value, "action", "selector", "key"); err != nil {
			return Action{}, err
		}
		selector, err := optionalString(value, "selector", "")
		if err != nil {
			return Action{}, err
		}
		if err := rejectNonCSSSelector(selector); err != nil {
			return Action{}, err
		}
		key, err := requiredString(value, "key")
		if err != nil {
			return Action{}, err
		}
		return Action{Kind: name, Press: &PressAction{Selector: selector, Key: key}}, nil
	case "wait":
		if err := validateBrowserKeys(value, "action", "value"); err != nil {
			return Action{}, err
		}
		ms, err := intArgRange(value, "value", -1, 0, 300000)
		if err != nil || ms < 0 {
			return Action{}, browserInvalid("wait requires integer value in milliseconds", nil)
		}
		return Action{Kind: name, Wait: &WaitAction{Duration: time.Duration(ms) * time.Millisecond}}, nil
	case "wait_for_selector":
		if err := validateBrowserKeys(value, "action", "selector", "state", "timeout_ms"); err != nil {
			return Action{}, err
		}
		selector, err := requiredCSSSelector(value)
		if err != nil {
			return Action{}, err
		}
		st, err := state()
		if err != nil {
			return Action{}, err
		}
		t, err := timeout()
		if err != nil {
			return Action{}, err
		}
		return Action{Kind: name, WaitSelector: &WaitSelectorAction{Selector: selector, State: st, Timeout: t}}, nil
	case "wait_for_url":
		if err := validateBrowserKeys(value, "action", "url", "timeout_ms"); err != nil {
			return Action{}, err
		}
		urlValue, err := requiredString(value, "url")
		if err != nil {
			return Action{}, err
		}
		t, err := timeout()
		if err != nil {
			return Action{}, err
		}
		return Action{Kind: name, WaitURL: &WaitURLAction{URL: urlValue, Timeout: t}}, nil
	case "wait_for_text":
		if err := validateBrowserKeys(value, "action", "text", "exact", "state", "timeout_ms"); err != nil {
			return Action{}, err
		}
		text, err := requiredString(value, "text")
		if err != nil {
			return Action{}, err
		}
		exact, err := boolArgStrict(value, "exact", false)
		if err != nil {
			return Action{}, err
		}
		st, err := state()
		if err != nil {
			return Action{}, err
		}
		t, err := timeout()
		if err != nil {
			return Action{}, err
		}
		return Action{Kind: name, WaitText: &WaitTextAction{Text: text, Exact: exact, State: st, Timeout: t}}, nil
	case "wait_for_response":
		if err := validateBrowserKeys(value, "action", "url", "url_pattern", "method", "status", "timeout_ms"); err != nil {
			return Action{}, err
		}
		urlValue, err := optionalString(value, "url", "")
		if err != nil {
			return Action{}, err
		}
		pattern, err := optionalString(value, "url_pattern", "")
		if err != nil {
			return Action{}, err
		}
		if urlValue == "" && pattern == "" {
			return Action{}, browserInvalid("wait_for_response requires url or url_pattern", nil)
		}
		if pattern != "" {
			if _, err := regexp.Compile(pattern); err != nil {
				return Action{}, browserInvalid("url_pattern must be a valid regular expression", map[string]any{"reason": err.Error()})
			}
		}
		method, err := optionalString(value, "method", "")
		if err != nil {
			return Action{}, err
		}
		method = strings.ToUpper(method)
		status := 0
		if _, exists := value["status"]; exists {
			status, err = intArgRange(value, "status", 0, 100, 599)
			if err != nil {
				return Action{}, err
			}
		}
		t, err := timeout()
		if err != nil {
			return Action{}, err
		}
		return Action{Kind: name, WaitResponse: &WaitResponseAction{URL: urlValue, URLPattern: pattern, Method: method, Status: status, Timeout: t}}, nil
	case "select":
		if err := validateBrowserKeys(value, "action", "selector", "value"); err != nil {
			return Action{}, err
		}
		selector, err := requiredCSSSelector(value)
		if err != nil {
			return Action{}, err
		}
		selected, err := requiredStringAllowEmpty(value, "value")
		if err != nil {
			return Action{}, err
		}
		return Action{Kind: name, Select: &SelectAction{Selector: selector, Value: selected}}, nil
	case "scroll":
		if err := validateBrowserKeys(value, "action", "delta_x", "delta_y"); err != nil {
			return Action{}, err
		}
		dx, err := intArgRange(value, "delta_x", 0, -100000, 100000)
		if err != nil {
			return Action{}, err
		}
		dy, err := intArgRange(value, "delta_y", 0, -100000, 100000)
		if err != nil {
			return Action{}, err
		}
		if dx == 0 && dy == 0 {
			return Action{}, browserInvalid("scroll requires a non-zero delta_x or delta_y", nil)
		}
		return Action{Kind: name, Scroll: &ScrollAction{DeltaX: int64(dx), DeltaY: int64(dy)}}, nil
	case "reload", "back", "forward":
		if err := validateBrowserKeys(value, "action", "wait_until", "timeout_ms"); err != nil {
			return Action{}, err
		}
		wait, err := waitUntil()
		if err != nil {
			return Action{}, err
		}
		t, err := timeout()
		if err != nil {
			return Action{}, err
		}
		return Action{Kind: name, Navigation: &NavigationAction{WaitUntil: wait, Timeout: t}}, nil
	default:
		return Action{}, browserInvalid("unknown browser action", map[string]any{"action": name})
	}
}

func validateBrowserKeys(values map[string]any, allowed ...string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range values {
		if _, ok := set[key]; !ok {
			return browserInvalid("unknown browser field", map[string]any{"field": key})
		}
	}
	return nil
}

func requiredCSSSelector(values map[string]any) (string, error) {
	selector, err := requiredString(values, "selector")
	if err != nil {
		return "", err
	}
	if err := rejectNonCSSSelector(selector); err != nil {
		return "", err
	}
	return selector, nil
}

func rejectNonCSSSelector(selector string) error {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		return nil
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(lower, "xpath=") || strings.HasPrefix(lower, "text=") || strings.HasPrefix(lower, "role=") {
		return browserInvalid("selector must be CSS", map[string]any{"selector": selector})
	}
	return nil
}

func requiredString(values map[string]any, key string) (string, error) {
	value, err := requiredStringAllowEmpty(values, key)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", browserInvalid(key+" is required", map[string]any{"field": key})
	}
	return strings.TrimSpace(value), nil
}

func requiredStringAllowEmpty(values map[string]any, key string) (string, error) {
	raw, ok := values[key]
	if !ok {
		return "", browserInvalid(key+" is required", map[string]any{"field": key})
	}
	value, ok := raw.(string)
	if !ok {
		return "", browserInvalid(key+" must be a string", map[string]any{"field": key})
	}
	return value, nil
}

func optionalString(values map[string]any, key, fallback string) (string, error) {
	raw, ok := values[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", browserInvalid(key+" must be a string", map[string]any{"field": key})
	}
	return strings.TrimSpace(value), nil
}

func boolArgStrict(values map[string]any, key string, fallback bool) (bool, error) {
	raw, ok := values[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, browserInvalid(key+" must be a boolean", map[string]any{"field": key})
	}
	return value, nil
}

func intArgRange(values map[string]any, key string, fallback, minimum, maximum int) (int, error) {
	raw, ok := values[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	value, ok := integerValue(raw)
	if !ok || value < int64(minimum) || value > int64(maximum) {
		return 0, browserInvalid(fmt.Sprintf("%s must be an integer between %d and %d", key, minimum, maximum), map[string]any{"field": key})
	}
	return int(value), nil
}

func durationArg(values map[string]any, key string, fallback, unit, maximum time.Duration) (time.Duration, error) {
	fallbackUnits := int64(fallback / unit)
	maxUnits := int(maximum / unit)
	value, err := intArgRange(values, key, int(fallbackUnits), 1, maxUnits)
	if err != nil {
		return 0, err
	}
	return time.Duration(value) * unit, nil
}

func floatArg(values map[string]any, key string, fallback float64) (float64, error) {
	raw, ok := values[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	switch value := raw.(type) {
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, browserInvalid(key+" must be finite", nil)
		}
		return value, nil
	case float32:
		return float64(value), nil
	case int:
		return float64(value), nil
	case int64:
		return float64(value), nil
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return 0, browserInvalid(key+" must be numeric", nil)
		}
		return parsed, nil
	default:
		return 0, browserInvalid(key+" must be numeric", map[string]any{"field": key})
	}
}

func integerValue(raw any) (int64, bool) {
	switch value := raw.(type) {
	case int:
		return int64(value), true
	case int8:
		return int64(value), true
	case int16:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case uint:
		if uint64(value) <= math.MaxInt64 {
			return int64(value), true
		}
	case uint64:
		if value <= math.MaxInt64 {
			return int64(value), true
		}
	case float64:
		if math.Trunc(value) == value && value >= math.MinInt64 && value <= math.MaxInt64 {
			return int64(value), true
		}
	case json.Number:
		parsed, err := value.Int64()
		return parsed, err == nil
	}
	return 0, false
}
