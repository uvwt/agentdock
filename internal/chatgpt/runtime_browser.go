package chatgpt

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ToolCaller is the subset of AgentDock runtime needed by the browser adapter.
// Implemented by tools.Runtime (via thin adapter to avoid import cycles).
type ToolCaller interface {
	Call(ctx context.Context, name string, args map[string]any) (map[string]any, error)
}

// RuntimeBrowser adapts AgentDock browser_* tools to the Loop Browser interface.
// Requires browser tools enabled. Locators stay here so ChatGPT UI churn stays out of Goal Core.
type RuntimeBrowser struct {
	Caller           ToolCaller
	ProfileID        string
	Headless         bool
	AutoApproveTools bool
	sessionID        string
	pageID           string
}

// Default ChatGPT locators — multi-layer fallbacks; not part of Goal state.
var (
	chatgptURL        = "https://chatgpt.com/"
	newChatSelectors  = []string{`a[href="/"]`, `button[data-testid="create-new-chat-button"]`, `nav a[href="/"]`}
	composerSelectors = []string{`#prompt-textarea`, `div[contenteditable="true"]#prompt-textarea`, `textarea[data-id="root"]`, `[data-testid="composer-text-input"]`}
	sendSelectors     = []string{`button[data-testid="send-button"]`, `button[aria-label="Send prompt"]`, `form button[type="submit"]`}
)

// Reset drops cached session/page ids so the next EnsureSession rebinds to a live browser page.
// Call this after ForceRotate or when the runner reports BROWSER_PAGE_NOT_FOUND.
func (b *RuntimeBrowser) Reset() {
	if b == nil {
		return
	}
	b.sessionID = ""
	b.pageID = ""
}

func (b *RuntimeBrowser) EnsureSession(ctx context.Context, profileID string) error {
	if b.Caller == nil {
		return fmt.Errorf("browser tool caller is nil")
	}
	if profileID == "" {
		profileID = b.ProfileID
	}
	if profileID == "" {
		profileID = "chatgpt"
	}
	b.ProfileID = profileID

	// Drop stale page binding before start. page_id is process-local in the browser-runner;
	// reusing a previous page-N after CDP restart yields "Unknown browser page_id".
	b.pageID = ""

	// keep_open + headless=false is required for the runner to actually launch a visible
	// Chrome/Chromium process on session_start. Without keep_open it only writes state JSON.
	args := map[string]any{
		"action":     "start",
		"profile_id": profileID,
		"browser":    "chrome",
		"headless":   b.Headless,
		"keep_open":  true,
		"url":        chatgptURL,
	}
	res, err := b.Caller.Call(ctx, "browser_session", args)
	if err != nil || browserResultFailed(res) {
		// Fallback without forcing chrome channel / after protocol glitch.
		delete(args, "browser")
		b.Reset()
		res, err = b.Caller.Call(ctx, "browser_session", args)
		if err != nil {
			return err
		}
	}
	if ok, _ := res["browser_ok"].(bool); !ok && res["browser_ok"] != nil {
		if msg := firstString(res, "browser_error", "error"); msg != "" {
			return fmt.Errorf("browser_session failed: %s", msg)
		}
	}
	if id := firstString(res, "session_id"); id != "" {
		b.sessionID = id
	}
	if id := firstString(res, "page_id"); id != "" {
		b.pageID = id
	}
	// If start only created metadata (no page yet), force a real navigation launch.
	if b.pageID == "" {
		if err := b.OpenChatGPT(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (b *RuntimeBrowser) OpenChatGPT(ctx context.Context) error {
	// Only navigate when we don't already have a live page binding. Blind goto to
	// https://chatgpt.com/ abandons the active conversation and looks like "new session spam".
	if b.pageID != "" && b.sessionID != "" {
		return nil
	}
	return b.act(ctx, []map[string]any{{
		"action": "goto", "url": chatgptURL,
	}})
}

func (b *RuntimeBrowser) OpenConversation(ctx context.Context, conversationURL string) error {
	conversationURL = strings.TrimSpace(conversationURL)
	if conversationURL == "" {
		return b.OpenChatGPT(ctx)
	}
	// If we are already on this thread, do not reload (would interrupt streaming/tool use).
	if cur, err := b.CurrentURL(ctx); err == nil {
		if sameChatGPTThread(cur, conversationURL) {
			return nil
		}
		// If already on any chatgpt page and navigation is flaky, prefer staying put
		// only when URL matches; otherwise try navigate with soft recovery.
	}
	err := b.act(ctx, []map[string]any{{
		"action": "goto", "url": conversationURL,
	}})
	if err == nil {
		return nil
	}
	// CDP/Page.navigate timeouts are common after rotate; soft-recover instead of
	// failing the whole wake. Prefer active ChatGPT tab if usable, else new chat.
	low := strings.ToLower(err.Error())
	navTimeout := strings.Contains(low, "timeout") || strings.Contains(low, "page.navigate") || strings.Contains(low, "invalid json")
	if !navTimeout {
		return err
	}
	if cur, err2 := b.CurrentURL(ctx); err2 == nil {
		if sameChatGPTThread(cur, conversationURL) {
			return nil
		}
		if strings.Contains(strings.ToLower(cur), "chatgpt.com") {
			// Active tab is ChatGPT but not the bound id (or id unreadable). Continue
			// with current tab rather than hard-failing resume delivery.
			return nil
		}
	}
	if _, err3 := b.NewConversation(ctx); err3 == nil {
		return nil
	}
	// Last resort: home page.
	if err4 := b.OpenChatGPT(ctx); err4 == nil {
		return nil
	}
	return fmt.Errorf("open conversation: %w", err)
}

func (b *RuntimeBrowser) CurrentURL(ctx context.Context) (string, error) {
	snap, err := b.snapshot(ctx)
	if err != nil {
		return "", err
	}
	candidates := []string{}
	if u := firstString(snap, "url", "page_url"); u != "" {
		candidates = append(candidates, u)
	}
	if page, ok := snap["page"].(map[string]any); ok {
		if u := firstString(page, "url"); u != "" {
			candidates = append(candidates, u)
		}
	}
	for _, u := range candidates {
		// Never treat CDP/Playwright target ids as page URLs.
		if strings.HasPrefix(strings.ToLower(u), "http://") || strings.HasPrefix(strings.ToLower(u), "https://") {
			return u, nil
		}
	}
	// Fallback: ask the page directly.
	res, err := b.Caller.Call(ctx, "browser_act", b.pageArgs(map[string]any{
		"actions": []map[string]any{{
			"action": "evaluate", "expression": "location.href",
		}},
		"keep_open": true,
		"headless":  b.Headless,
	}))
	if err == nil {
		// evaluate results may appear under different keys depending on runner.
		for _, key := range []string{"result", "value", "text", "url"} {
			if u := firstString(res, key); strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
				return u, nil
			}
		}
		// nested evaluate payload
		if ev, ok := res["evaluate"].(map[string]any); ok {
			if u := firstString(ev, "result", "value"); strings.HasPrefix(u, "http") {
				return u, nil
			}
		}
	}
	return "", fmt.Errorf("page url unavailable")
}

func sameChatGPTThread(a, b string) bool {
	return conversationIDFromURL(a) != "" && conversationIDFromURL(a) == conversationIDFromURL(b)
}

func (b *RuntimeBrowser) NewConversation(ctx context.Context) (string, error) {
	// Prefer the in-app "New chat" control. Avoid a second goto to / which races the click
	// and can open yet another blank root tab.
	b.pageID = ""
	if err := b.clickFirst(ctx, newChatSelectors); err != nil {
		// Fallback: open root only if we couldn't click New chat.
		_ = b.act(ctx, []map[string]any{{"action": "goto", "url": chatgptURL}})
	}
	// Give the SPA a moment to mount the empty composer before paste.
	_ = b.act(ctx, []map[string]any{{"action": "wait", "value": 800}})
	return fmt.Sprintf("chatgpt-%d", time.Now().UnixNano()), nil
}

// SoftRebind drops stale page_id only (keeps conversation tab). Used after transient
// CDP errors so the next act re-resolves the active page without opening a new chat.
func (b *RuntimeBrowser) SoftRebind() {
	if b == nil {
		return
	}
	b.pageID = ""
}

func (b *RuntimeBrowser) PasteAndSend(ctx context.Context, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("resume prompt is empty")
	}
	// Resume prompts can be multi-KB. Prefer CDP Input.insertText path in the runner
	// (via mode=cdp_input) so the body is not embedded into Runtime.evaluate source.
	fillTimeoutMS := 30000
	if n := len(text); n > 4000 {
		fillTimeoutMS = 60000
	}
	var last error
	for _, sel := range composerSelectors {
		err := b.act(ctx, []map[string]any{{
			"action": "fill", "selector": sel, "value": text,
			"timeout_ms": fillTimeoutMS, "mode": "cdp_input",
		}})
		if err == nil {
			last = nil
			break
		}
		last = err
	}
	if last != nil {
		_ = b.clickFirst(ctx, composerSelectors)
		last = b.act(ctx, []map[string]any{{
			"action": "fill", "selector": composerSelectors[0], "value": text,
			"timeout_ms": fillTimeoutMS, "mode": "cdp_input",
		}})
		if last != nil {
			return fmt.Errorf("fill composer: %w", last)
		}
	}
	if err := b.clickFirst(ctx, sendSelectors); err != nil {
		return b.act(ctx, []map[string]any{{
			"action": "press", "selector": composerSelectors[0], "key": "Enter",
			"timeout_ms": 15000, "mode": "cdp_input",
		}})
	}
	return nil
}

func (b *RuntimeBrowser) WaitIdle(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	// Require consecutive non-busy probes so we don't paste between tool calls.
	stable := 0
	snapshotFails := 0
	needStable := 2
	poll := 2 * time.Second
	// Short timeouts (tests / quick probes) must still complete a successful idle check.
	if timeout < 5*time.Second {
		poll = 100 * time.Millisecond
		needStable = 1
	}
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Selector-only busy probe first (cheap CDP). Never hammer a wedged renderer.
		busy, probeErr := b.pageBusy(ctx)
		if probeErr != nil {
			if isStalePageErr(probeErr) {
				b.pageID = ""
				return fmt.Errorf("page_stuck: stale page while waiting idle: %w", probeErr)
			}
			snapshotFails++
			// One-strike on CDP timeout / stuck renderer (r9: 3-strike still pinned Chrome).
			// Any probe failure after the first also fails closed — do not paste into a wedged tab.
			if snapshotFails >= 1 || isPageStuckErr(probeErr) {
				return fmt.Errorf("page_stuck: %w", probeErr)
			}
		} else if busy {
			stable = 0
			snapshotFails = 0
		} else {
			snapshotFails = 0
			stable++
			if stable >= needStable {
				return nil
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		wait := poll
		if wait > remaining {
			wait = remaining
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return fmt.Errorf("wait idle timeout after %s", timeout)
}

// pageBusy returns true when ChatGPT is still streaming / running tools / showing
// a permission modal. Prefer selector-only evaluate (no innerText scrape) to avoid
// pinning a busy SPA main thread (r9 re-wake freeze).
func (b *RuntimeBrowser) pageBusy(ctx context.Context) (bool, error) {
	// Cheap path: stop button + tool-action permission buttons only.
	res, err := b.Caller.Call(ctx, "browser_act", b.pageArgs(map[string]any{
		"actions": []map[string]any{{
			"action": "evaluate",
			"expression": `(() => {
  const stop = document.querySelector('[data-testid="stop-button"], button[aria-label*="Stop" i], button[aria-label*="停止"]');
  const hasToolButtons = !!document.querySelector('[data-testid="tool-action-buttons"]');
  return {busy: !!(stop || hasToolButtons), streaming: !!stop, tools: false, permission: !!hasToolButtons, cheap: true};
})()`,
			"timeout_ms": 4000,
		}},
		"keep_open": true,
		"headless":  b.Headless,
	}))
	if err != nil {
		return false, err
	}
	if ok, exists := res["browser_ok"].(bool); exists && !ok {
		msg := firstString(res, "browser_error", "error")
		if msg == "" {
			msg = "browser_act failed"
		}
		return false, fmt.Errorf("%s", msg)
	}
	if id := firstString(res, "page_id"); id != "" {
		b.pageID = id
	}
	if m := evaluateMap(res); m != nil {
		if busy, _ := m["busy"].(bool); busy {
			return true, nil
		}
		// Selectors say idle. Done — skip 12k innerText scrape (was freeze amplifier).
		return false, nil
	}
	blob := strings.ToLower(fmt.Sprint(res))
	if strings.Contains(blob, `"busy":true`) || strings.Contains(blob, "busy:true") {
		return true, nil
	}
	return false, nil
}

func (b *RuntimeBrowser) DetectBlockers(ctx context.Context) ([]string, error) {
	// Lightweight busy/permission probe first — avoids full snapshot when page is wedged.
	if busy, err := b.pageBusy(ctx); err != nil {
		if isStalePageErr(err) {
			b.pageID = ""
			return []string{"page_error"}, nil
		}
		if isPageStuckErr(err) {
			return []string{"page_stuck"}, nil
		}
		return []string{"snapshot_error:" + err.Error()}, nil
	} else if busy {
		// Distinguish permission vs general busy via a short snapshot only when needed.
		out := []string{"page_busy"}
		if snap, snapErr := b.snapshot(ctx); snapErr == nil {
			blob := strings.ToLower(fmt.Sprint(snap))
			if hasToolPermissionText(blob) {
				out = []string{"tool_permission"}
			}
		}
		if b.AutoApproveTools {
			if approved, aerr := b.approveToolPermission(ctx); aerr == nil && approved {
				return []string{"tool_permission_auto_approved"}, nil
			}
		}
		return out, nil
	}

	snap, err := b.snapshot(ctx)
	if err != nil {
		if isStalePageErr(err) {
			b.pageID = ""
			return []string{"page_error"}, nil
		}
		if isPageStuckErr(err) {
			return []string{"page_stuck"}, nil
		}
		return []string{"snapshot_error:" + err.Error()}, nil
	}
	blob := strings.ToLower(fmt.Sprint(snap))
	var out []string
	// Prefer page text only — full snapshot fmt.Sprint can include URLs/titles from
	// the sidebar (e.g. a chat named "Cloudflare Zero Trust DNS") and false-positive.
	text := strings.ToLower(firstString(snap, "text"))
	if text == "" {
		text = blob
	}
	title := strings.ToLower(firstString(snap, "title"))
	url := strings.ToLower(firstString(snap, "url"))
	if page, ok := snap["page"].(map[string]any); ok {
		if title == "" {
			title = strings.ToLower(firstString(page, "title"))
		}
		if url == "" {
			url = strings.ToLower(firstString(page, "url"))
		}
	}

	// Soft content checks on visible page text (not sidebar history titles alone).
	soft := []struct {
		needle string
		label  string
	}{
		{"usage limit", "usage limit"},
		{"rate limit", "rate limit"},
		{"you've hit the", "usage limit"},
		{"something went wrong", "something went wrong"},
		{"network error", "network error"},
	}
	for _, c := range soft {
		if strings.Contains(text, c.needle) {
			out = append(out, c.label)
		}
	}
	// Cloudflare / captcha: require challenge-shaped copy, not the bare word
	// "cloudflare" (sidebar chat titles and docs links false-positive hard).
	if isCloudflareChallenge(text, title, url) {
		out = append(out, "cloudflare challenge")
	}
	if strings.Contains(text, "verify you are human") || strings.Contains(text, "確認您是真人") {
		out = append(out, "captcha")
	}
	// Login: only if composer is absent-ish and sign-in copy dominates. Bare
	// "log in" appears in account menus on a healthy logged-in ChatGPT page.
	if isLoginRequired(text, title) {
		out = append(out, "login_required")
	}
	if hasToolPermissionText(text) || hasToolPermissionText(blob) {
		out = append(out, "tool_permission")
		if b.AutoApproveTools {
			if approved, aerr := b.approveToolPermission(ctx); aerr == nil && approved {
				filtered := out[:0]
				for _, x := range out {
					if x != "tool_permission" {
						filtered = append(filtered, x)
					}
				}
				out = append(filtered, "tool_permission_auto_approved")
			}
		}
	}
	return out, nil
}

// isCloudflareChallenge distinguishes a real interstitial from sidebar noise
// like a conversation titled "Cloudflare Zero Trust DNS".
func isCloudflareChallenge(text, title, url string) bool {
	// Real CF interstitial titles / URLs.
	if strings.Contains(title, "just a moment") || strings.Contains(title, "attention required") {
		return true
	}
	if strings.Contains(url, "cdn-cgi/challenge") || strings.Contains(url, "challenges.cloudflare.com") {
		return true
	}
	needles := []string{
		"checking your browser",
		"just a moment",
		"cf-browser-verification",
		"challenge-platform",
		"enable javascript and cookies to continue",
		"performing security verification",
		"needs to review the security of your connection",
		"確認您是真人",
		"正在驗證您是否是真人",
	}
	for _, n := range needles {
		if strings.Contains(text, n) {
			return true
		}
	}
	// Bare "cloudflare" alone is NOT enough (sidebar chat titles).
	return false
}

func isLoginRequired(text, title string) bool {
	if strings.Contains(title, "log in") || strings.Contains(title, "sign in") ||
		strings.Contains(title, "登入") || strings.Contains(title, "登录") {
		return true
	}
	// Healthy ChatGPT home has the composer prompt; do not treat account menu as login wall.
	if strings.Contains(text, "我們該從哪裡開始") || strings.Contains(text, "where should we start") ||
		strings.Contains(text, "ask anything") || strings.Contains(text, "message chatgpt") {
		return false
	}
	// Strong login-wall phrases only.
	strong := []string{
		"log in to continue",
		"sign in to continue",
		"請登入以繼續",
		"请登录以继续",
		"create an account",
		"welcome back",
	}
	for _, n := range strong {
		if strings.Contains(text, n) {
			return true
		}
	}
	return false
}

func hasToolPermissionText(blob string) bool {
	blob = strings.ToLower(blob)
	if strings.Contains(blob, "data-testid=\"tool-action-buttons\"") || strings.Contains(blob, "tool-action-buttons") {
		return true
	}
	if strings.Contains(blob, "svananda") && (strings.Contains(blob, "允許") || strings.Contains(blob, "允许") || strings.Contains(blob, "allow")) {
		return true
	}
	return strings.Contains(blob, "要允許") ||
		strings.Contains(blob, "要允许") ||
		strings.Contains(blob, "allow chatgpt to use") ||
		strings.Contains(blob, "always allow") ||
		(strings.Contains(blob, "要允許 chatgpt 使用") || strings.Contains(blob, "要允许 chatgpt 使用")) ||
		(strings.Contains(blob, "允許") && strings.Contains(blob, "使用") && strings.Contains(blob, "嗎"))
}

// approveToolPermission clicks localized Allow buttons. Prefer the Svananda /
// tool-action-buttons primary 「允許」 control from live ChatGPT UI.
// Success requires the permission dialog text to be gone after click (r7 sticky
// auto_approved false-positive fix).
func (b *RuntimeBrowser) approveToolPermission(ctx context.Context) (bool, error) {
	res, err := b.Caller.Call(ctx, "browser_act", b.pageArgs(map[string]any{
		"actions": []map[string]any{{
			"action": "evaluate",
			"expression": `(() => {
  const denyExact = new Set(['取消', 'Cancel', '拒絕', '拒绝', 'Deny', 'Not now', '稍後', '稍后']);
  const root = document.querySelector('[data-testid="tool-action-buttons"]') || document.body;
  // Prefer remembering this chat so later tool calls do not re-prompt.
  const remember = document.querySelector('#dont-ask-again, [data-testid="dont-ask-again"]');
  if (remember && !remember.checked) {
    try { remember.click(); } catch {}
  }
  const buttons = Array.from((root || document).querySelectorAll('button, [role="button"]'));
  const textOf = (el) => (el.innerText || el.textContent || '').trim();
  // 1) Exact primary allow inside tool-action-buttons.
  const exact = buttons.find(b => {
    const t = textOf(b);
    return t === '允許' || t === '允许' || t === 'Allow' || t === 'Always allow' || t === '允許此對話' || t === 'Allow for this chat';
  });
  if (exact) { exact.click(); return {ok:true, label: textOf(exact), scope:'tool-action-buttons'}; }
  // 2) Primary/btn-primary with allow-ish text.
  const primary = buttons.find(b => {
    const t = textOf(b);
    if (!t || denyExact.has(t)) return false;
    const cls = (b.className || '').toString();
    const isPrimary = cls.includes('btn-primary') || b.getAttribute('data-testid') === 'confirm-button';
    return isPrimary && (t.includes('允許') || t.includes('允许') || /^Allow\b/i.test(t));
  });
  if (primary) { primary.click(); return {ok:true, label: textOf(primary), scope:'primary'}; }
  // 3) Any button whose visible text is allow (not deny).
  const labels = ['Always allow', '允許此對話', '允许此对话', '允許', '允许', 'Allow for this chat', 'Allow'];
  for (const label of labels) {
    const btn = buttons.find(b => {
      const t = textOf(b);
      if (!t || !t.includes(label)) return false;
      if (denyExact.has(t) || t.startsWith('拒絕') || t.startsWith('拒绝') || t.startsWith('Cancel')) return false;
      return true;
    });
    if (btn) { btn.click(); return {ok:true, label, scope:'fallback'}; }
  }
  return {ok:false, buttons: buttons.map(textOf).filter(Boolean).slice(0, 12)};
})()`,
			"timeout_ms": 8000,
		}},
		"keep_open": true,
		"headless":  b.Headless,
	}))
	if err != nil {
		return false, err
	}
	clicked := false
	if m := evaluateMap(res); m != nil {
		clicked, _ = m["ok"].(bool)
	} else {
		blob := strings.ToLower(fmt.Sprint(res))
		clicked = strings.Contains(blob, `"ok":true`) || strings.Contains(blob, "ok:true")
	}
	if !clicked {
		return false, nil
	}
	// Brief settle, then one cheap verify. If CDP is already wedging after the
	// click, trust the click and hands-off rather than retry-storming evaluate
	// (r8: post-allow dismiss verify timed out while renderer spun at 100%).
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-time.After(300 * time.Millisecond):
	}
	still, err := b.permissionDialogPresent(ctx)
	if err != nil {
		// Click landed; page too busy to re-probe — accept approval and stop CDP.
		return true, nil
	}
	if still {
		return false, nil
	}
	return true, nil
}

// permissionDialogPresent reports whether the tool-permission UI is still visible.
func (b *RuntimeBrowser) permissionDialogPresent(ctx context.Context) (bool, error) {
	res, err := b.Caller.Call(ctx, "browser_act", b.pageArgs(map[string]any{
		"actions": []map[string]any{{
			"action": "evaluate",
			"expression": `(() => {
  const t = ((document.body && document.body.innerText) || '').slice(0, 12000);
  const hasButtons = !!document.querySelector('[data-testid="tool-action-buttons"]');
  const hasCopy = /要允許\s*ChatGPT\s*使用/i.test(t) || /要允许\s*ChatGPT\s*使用/i.test(t) ||
    /allow chatgpt to use/i.test(t) || (/Svananda/i.test(t) && /要允許|要允许|Allow/i.test(t) && /拒絕|拒绝|Deny|允許|允许/.test(t));
  return {present: !!(hasButtons || hasCopy)};
})()`,
			"timeout_ms": 5000,
		}},
		"keep_open": true,
		"headless":  b.Headless,
	}))
	if err != nil {
		return false, err
	}
	if ok, exists := res["browser_ok"].(bool); exists && !ok {
		msg := firstString(res, "browser_error", "error")
		if msg == "" {
			msg = "browser_act failed"
		}
		return false, fmt.Errorf("%s", msg)
	}
	if m := evaluateMap(res); m != nil {
		if p, _ := m["present"].(bool); p {
			return true, nil
		}
		return false, nil
	}
	blob := strings.ToLower(fmt.Sprint(res))
	return strings.Contains(blob, `"present":true`) || strings.Contains(blob, "present:true"), nil
}

func (b *RuntimeBrowser) clickFirst(ctx context.Context, selectors []string) error {
	var last error
	for _, sel := range selectors {
		err := b.act(ctx, []map[string]any{{"action": "click", "selector": sel}})
		if err == nil {
			return nil
		}
		last = err
	}
	if last == nil {
		return fmt.Errorf("no selectors")
	}
	return last
}

func (b *RuntimeBrowser) snapshot(ctx context.Context) (map[string]any, error) {
	// Never screenshot on routine probes — Page.captureScreenshot on a busy ChatGPT
	// tab is a main-thread / compositor tax that helped pin the renderer at 100% CPU.
	args := b.pageArgs(map[string]any{
		"capture_screenshot": false,
		"keep_open":          true,
		"headless":           b.Headless,
	})
	res, err := b.Caller.Call(ctx, "browser_snapshot", args)
	if err != nil {
		if isStalePageErr(err) && b.pageID != "" {
			b.pageID = ""
			res, err = b.Caller.Call(ctx, "browser_snapshot", b.pageArgs(map[string]any{
				"capture_screenshot": false,
				"keep_open":          true,
				"headless":           b.Headless,
			}))
		}
		if err != nil {
			return nil, err
		}
	}
	if ok, exists := res["browser_ok"].(bool); exists && !ok {
		msg := firstString(res, "browser_error", "error")
		if isStalePageErr(fmt.Errorf("%s", msg)) && b.pageID != "" {
			b.pageID = ""
			res, err = b.Caller.Call(ctx, "browser_snapshot", b.pageArgs(map[string]any{
				"capture_screenshot": false,
				"keep_open":          true,
				"headless":           b.Headless,
			}))
			if err != nil {
				return nil, err
			}
		} else if msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
	}
	if id := firstString(res, "page_id"); id != "" {
		b.pageID = id
	}
	if id := firstString(res, "session_id"); id != "" {
		b.sessionID = id
	}
	return res, nil
}

func (b *RuntimeBrowser) act(ctx context.Context, actions []map[string]any) error {
	err := b.actOnce(ctx, actions)
	if err == nil {
		return nil
	}
	// Only soft-rebind once for stale page ids. Do NOT Reset/EnsureSession/OpenChatGPT
	// on timeouts: that hammers a wedged ChatGPT renderer and opens stray tabs.
	if !isStalePageErr(err) {
		return err
	}
	b.pageID = ""
	if err2 := b.actOnce(ctx, actions); err2 == nil {
		return nil
	} else {
		return err2
	}
}

func (b *RuntimeBrowser) actOnce(ctx context.Context, actions []map[string]any) error {
	args := b.pageArgs(map[string]any{
		"actions":   actions,
		"keep_open": true,
		"headless":  b.Headless,
	})
	if b.ProfileID != "" {
		args["profile_id"] = b.ProfileID
	}
	res, err := b.Caller.Call(ctx, "browser_act", args)
	if err != nil {
		return err
	}
	if ok, exists := res["browser_ok"].(bool); exists && !ok {
		if msg := firstString(res, "browser_error", "error"); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return fmt.Errorf("browser_act failed")
	}
	if id := firstString(res, "page_id"); id != "" {
		b.pageID = id
	}
	if id := firstString(res, "session_id"); id != "" {
		b.sessionID = id
	}
	return nil
}

func (b *RuntimeBrowser) pageArgs(extra map[string]any) map[string]any {
	args := map[string]any{}
	for k, v := range extra {
		args[k] = v
	}
	if b.sessionID != "" {
		args["session_id"] = b.sessionID
	}
	if b.pageID != "" {
		args["page_id"] = b.pageID
	}
	return args
}

func isStalePageErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown browser page_id") ||
		strings.Contains(msg, "browser_page_not_found") ||
		strings.Contains(msg, "no debuggable page") ||
		strings.Contains(msg, "browser_page_unavailable")
}

// isPageStuckErr is true when CDP/evaluate timed out or the page main thread is unresponsive.
// Callers must NOT paste or hard-rebind on this — that freezes Chrome harder.
func isPageStuckErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "page_stuck") ||
		strings.Contains(msg, "cdp method timed out") ||
		strings.Contains(msg, "runtime.evaluate") && strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "browser runner returned invalid json") ||
		strings.Contains(msg, "browser_runner_protocol_error")
}

func evaluateMap(res map[string]any) map[string]any {
	if res == nil {
		return nil
	}
	for _, key := range []string{"result", "value", "evaluate"} {
		if m, ok := res[key].(map[string]any); ok {
			// Nested {result:{value:{...}}} from CDP wrappers.
			if inner, ok := m["value"].(map[string]any); ok {
				return inner
			}
			if inner, ok := m["result"].(map[string]any); ok {
				if v, ok := inner["value"].(map[string]any); ok {
					return v
				}
				return inner
			}
			return m
		}
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch t := v.(type) {
			case string:
				if strings.TrimSpace(t) != "" {
					return t
				}
			case map[string]any:
				if msg, ok := t["message"].(string); ok && msg != "" {
					return msg
				}
			default:
				s := strings.TrimSpace(fmt.Sprint(v))
				if s != "" && s != "<nil>" {
					return s
				}
			}
		}
	}
	return ""
}

func browserResultFailed(res map[string]any) bool {
	if res == nil {
		return true
	}
	if ok, exists := res["browser_ok"].(bool); exists && !ok {
		return true
	}
	if msg := firstString(res, "browser_error", "error"); msg != "" {
		low := strings.ToLower(msg)
		return strings.Contains(low, "invalid json") || strings.Contains(low, "protocol")
	}
	return false
}
