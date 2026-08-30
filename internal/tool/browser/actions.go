package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func (s *Service) act(ctx context.Context, req ActRequest) (Snapshot, error) {
	sess, err := s.getSession(req.SessionID)
	if err != nil {
		return Snapshot{}, err
	}
	sess.opMu.Lock()
	defer sess.opMu.Unlock()
	sess.touch(s.now())

	operationCtx, cancel := withTimeout(ctx, req.Timeout, 30*time.Second)
	defer cancel()
	requestedPageID := strings.TrimSpace(req.PageID)
	currentPage, err := sess.selectPage(requestedPageID)
	if err != nil {
		return Snapshot{}, err
	}

	diag := newDiagnostics()
	contexts := make(map[target.ID]context.Context)
	listenerCancels := make(map[target.ID]context.CancelFunc)
	defer func() {
		for _, cancel := range listenerCancels {
			cancel()
		}
	}()
	pageContext := func(id target.ID) (context.Context, error) {
		if existing := contexts[id]; existing != nil {
			return existing, nil
		}
		pageCtx, err := sess.ensurePageContext(operationCtx, id)
		if err != nil {
			return nil, classifyOperationError(err, "page_attach")
		}
		listenCtx, listenCancel := context.WithCancel(pageCtx)
		diag.attach(listenCtx)
		if err := diag.enable(operationCtx, pageCtx); err != nil {
			listenCancel()
			return nil, classifyOperationError(err, "monitor_enable")
		}
		contexts[id] = pageCtx
		listenerCancels[id] = listenCancel
		return pageCtx, nil
	}

	if _, err := pageContext(currentPage); err != nil {
		return Snapshot{}, err
	}
	for index, action := range req.Actions {
		pageCtx, err := pageContext(currentPage)
		if err != nil {
			return Snapshot{}, err
		}
		if err := runAction(operationCtx, pageCtx, diag, action); err != nil {
			return Snapshot{}, wrapActionError(err, index, action.Kind)
		}
		if err := sess.refreshPages(); err != nil {
			return Snapshot{}, browserError(ErrCDPFailed, "refresh browser pages after action", "cdp", &ErrorDetails{ActionIndex: intPointer(index)}, err)
		}
		if requestedPageID != "" {
			// 显式 page_id 是整次 browser_act 的稳定目标；即使动作打开了新标签页，也不能静默漂移。
			currentPage, err = sess.selectPage(requestedPageID)
			if err != nil {
				return Snapshot{}, wrapActionError(err, index, action.Kind)
			}
		} else if next, selectErr := sess.selectPage(""); selectErr == nil && next != "" {
			currentPage = next
		}
	}

	consoleErrors, networkErrors, pageErrors := diag.snapshot()
	snapshot, err := s.snapshotLocked(operationCtx, sess, SnapshotRequest{
		SessionID:              req.SessionID,
		PageID:                 string(currentPage),
		FullPage:               req.FullPage,
		MaxTextChars:           req.MaxTextChars,
		MaxInteractiveElements: req.MaxInteractiveElements,
		Timeout:                req.Timeout,
	}, consoleErrors, networkErrors, pageErrors)
	if err != nil {
		return Snapshot{}, err
	}
	if req.CloseAfter {
		if _, err := s.removeSession(req.SessionID); err != nil {
			return Snapshot{}, err
		}
		sess.stop()
		s.releaseSessionProfile(sess)
	}
	return snapshot, nil
}

func runAction(parent, pageCtx context.Context, diag *diagnostics, action Action) error {
	switch action.Kind {
	case "goto":
		a := action.Goto
		return navigateAndWait(parent, pageCtx, a.WaitUntil, a.Timeout, func(ctx context.Context) error {
			_, _, _, _, err := page.Navigate(a.URL).Do(ctx)
			return err
		})
	case "click":
		return runWithContext(parent, pageCtx, chromedp.Click(action.Click.Selector, chromedp.ByQuery))
	case "fill":
		return fillValue(parent, pageCtx, *action.Fill)
	case "press":
		a := action.Press
		tasks := chromedp.Tasks{}
		if a.Selector != "" {
			tasks = append(tasks, chromedp.Focus(a.Selector, chromedp.ByQuery))
		}
		tasks = append(tasks, chromedp.KeyEvent(keyValue(a.Key)))
		return runWithContext(parent, pageCtx, tasks...)
	case "wait":
		return waitDuration(parent, action.Wait.Duration)
	case "wait_for_selector":
		return waitForSelector(parent, pageCtx, *action.WaitSelector)
	case "wait_for_url":
		return waitForURL(parent, pageCtx, *action.WaitURL)
	case "wait_for_text":
		return waitForText(parent, pageCtx, *action.WaitText)
	case "wait_for_response":
		return waitForResponse(parent, diag, *action.WaitResponse)
	case "select":
		return selectValue(parent, pageCtx, *action.Select)
	case "scroll":
		return scrollBy(parent, pageCtx, *action.Scroll)
	case "reload":
		a := action.Navigation
		return navigateAndWait(parent, pageCtx, a.WaitUntil, a.Timeout, func(ctx context.Context) error { return page.Reload().Do(ctx) })
	case "back":
		return navigateHistory(parent, pageCtx, -1, *action.Navigation)
	case "forward":
		return navigateHistory(parent, pageCtx, 1, *action.Navigation)
	default:
		return browserError(ErrActionInvalid, "unsupported browser action", "validation", &ErrorDetails{Action: action.Kind}, nil)
	}
}

func navigateAndWait(parent, pageCtx context.Context, waitUntil WaitUntil, actionTimeout time.Duration, initiate func(context.Context) error) error {
	if waitUntil == "" {
		waitUntil = WaitLoad
	}
	ctx, cancel := operationContext(parent, pageCtx, actionTimeout)
	defer cancel()
	event := make(chan struct{}, 1)
	chromedp.ListenTarget(ctx, func(ev any) {
		matched := false
		switch ev.(type) {
		case *page.EventDomContentEventFired:
			matched = waitUntil == WaitDOMContentLoaded
		case *page.EventLoadEventFired:
			matched = waitUntil == WaitLoad
		}
		if matched {
			select {
			case event <- struct{}{}:
			default:
			}
		}
	})
	if err := chromedp.Run(ctx, page.Enable(), page.SetLifecycleEventsEnabled(true), chromedp.ActionFunc(initiate)); err != nil {
		return err
	}
	select {
	case <-event:
		return nil
	case <-ctx.Done():
		return browserError(ErrTimeout, "timed out waiting for navigation lifecycle event", "navigation", &ErrorDetails{WaitUntil: waitUntil}, ctx.Err())
	}
}

func navigateHistory(parent, pageCtx context.Context, delta int64, action NavigationAction) error {
	return navigateAndWait(parent, pageCtx, action.WaitUntil, action.Timeout, func(ctx context.Context) error {
		current, entries, err := page.GetNavigationHistory().Do(ctx)
		if err != nil {
			return err
		}
		next := current + delta
		if next < 0 || next >= int64(len(entries)) {
			return browserError(ErrActionFailed, "browser history entry is not available", "action", &ErrorDetails{Direction: delta}, nil)
		}
		return page.NavigateToHistoryEntry(entries[next].ID).Do(ctx)
	})
}

func waitDuration(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitForSelector(parent, pageCtx context.Context, action WaitSelectorAction) error {
	return poll(parent, action.Timeout, func() (bool, error) {
		var state struct {
			Found   bool `json:"found"`
			Visible bool `json:"visible"`
		}
		expr := elementStateExpression(action.Selector)
		if err := runWithContext(parent, pageCtx, chromedp.Evaluate(expr, &state)); err != nil {
			return false, err
		}
		return elementStateMatches(state.Found, state.Visible, action.State), nil
	})
}

func waitForURL(parent, pageCtx context.Context, action WaitURLAction) error {
	return poll(parent, action.Timeout, func() (bool, error) {
		var current string
		if err := runWithContext(parent, pageCtx, chromedp.Location(&current)); err != nil {
			return false, err
		}
		return urlMatches(current, action.URL), nil
	})
}

func waitForText(parent, pageCtx context.Context, action WaitTextAction) error {
	return poll(parent, action.Timeout, func() (bool, error) {
		var result bool
		textJSON, _ := json.Marshal(action.Text)
		stateJSON, _ := json.Marshal(string(action.State))
		expr := fmt.Sprintf(`(() => {
const expected = %s;
const exact = %t;
const wanted = %s;
const norm = value => String(value || '').replace(/\s+/g, ' ').trim();
const visible = el => {
  const r=el.getBoundingClientRect(); if(r.width<=0 || r.height<=0) return false;
  for(let cur=el; cur; cur=cur.parentElement) { const s=getComputedStyle(cur); if(s.display==='none' || s.visibility==='hidden' || s.visibility==='collapse' || Number(s.opacity)===0) return false; }
  return true;
};
const elements = Array.from(document.querySelectorAll('body *'));
const matches = elements.filter(el => { const text=norm(el.textContent); return exact ? text===norm(expected) : text.includes(norm(expected)); });
if (wanted === 'detached') return matches.length === 0;
if (wanted === 'attached') return matches.length > 0;
if (wanted === 'visible') return matches.some(visible);
if (wanted === 'hidden') return matches.length === 0 || matches.every(el => !visible(el));
return false;
})()`, textJSON, action.Exact, stateJSON)
		if err := runWithContext(parent, pageCtx, chromedp.Evaluate(expr, &result)); err != nil {
			return false, err
		}
		return result, nil
	})
}

func waitForResponse(parent context.Context, diag *diagnostics, action WaitResponseAction) error {
	ctx, cancel := withTimeout(parent, action.Timeout, 10*time.Second)
	defer cancel()
	for {
		if diag.hasMatchingResponse(action) {
			return nil
		}
		select {
		case <-diag.responseWake:
		case <-ctx.Done():
			return browserError(ErrTimeout, "timed out waiting for matching network response", "wait_for_response", responseWaitDetails(action), ctx.Err())
		}
	}
}

func fillValue(parent, pageCtx context.Context, action FillAction) error {
	selector, _ := json.Marshal(action.Selector)
	value, _ := json.Marshal(action.Value)
	expr := fmt.Sprintf(`(() => {
  const el = document.querySelector(%s);
  if (!el) return {ok:false, reason:'target not found'};
  if (el.disabled || el.readOnly) return {ok:false, reason:'target is not editable'};
  const value = %s;
  el.focus();
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) {
    const prototype = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    const setter = Object.getOwnPropertyDescriptor(prototype, 'value')?.set;
    if (!setter) return {ok:false, reason:'native value setter is unavailable'};
    setter.call(el, value);
  } else if (el.isContentEditable) {
    el.textContent = value;
  } else {
    return {ok:false, reason:'target is not editable'};
  }
  el.dispatchEvent(new Event('input', {bubbles:true}));
  el.dispatchEvent(new Event('change', {bubbles:true}));
  return {ok:true, reason:''};
})()`, selector, value)
	var result struct {
		OK     bool   `json:"ok"`
		Reason string `json:"reason"`
	}
	if err := runWithContext(parent, pageCtx, chromedp.Evaluate(expr, &result)); err != nil {
		return err
	}
	if !result.OK {
		return browserError(ErrActionFailed, "browser fill target is not editable", "action", &ErrorDetails{Selector: action.Selector, Reason: result.Reason}, nil)
	}
	return nil
}

func selectValue(parent, pageCtx context.Context, action SelectAction) error {
	selector, _ := json.Marshal(action.Selector)
	value, _ := json.Marshal(action.Value)
	expr := fmt.Sprintf(`(() => {
  const el = document.querySelector(%s);
  if (!el || el.tagName !== 'SELECT') return {ok:false, reason:'select target not found', selected:''};
  const value = %s;
  if (!Array.from(el.options).some(option => option.value === value)) return {ok:false, reason:'option value not found', selected:el.value};
  el.value = value;
  el.dispatchEvent(new Event('input', {bubbles:true}));
  el.dispatchEvent(new Event('change', {bubbles:true}));
  return {ok:el.value === value, reason:el.value === value ? '' : 'selected value did not change', selected:el.value};
})()`, selector, value)
	var result struct {
		OK     bool   `json:"ok"`
		Reason string `json:"reason"`
	}
	if err := runWithContext(parent, pageCtx, chromedp.Evaluate(expr, &result)); err != nil {
		return err
	}
	if !result.OK {
		return browserError(ErrActionFailed, "browser select value is unavailable", "action", &ErrorDetails{Selector: action.Selector, Reason: result.Reason}, nil)
	}
	return nil
}

func scrollBy(parent, pageCtx context.Context, action ScrollAction) error {
	expr := fmt.Sprintf("window.scrollBy(%d,%d)", action.DeltaX, action.DeltaY)
	return runWithContext(parent, pageCtx, chromedp.Evaluate(expr, nil))
}

func poll(parent context.Context, timeout time.Duration, check func() (bool, error)) error {
	ctx, cancel := withTimeout(parent, timeout, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		ok, err := check()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return browserError(ErrTimeout, "browser wait timed out", "wait", nil, ctx.Err())
		}
	}
}

func elementStateExpression(selector string) string {
	encoded, _ := json.Marshal(selector)
	return fmt.Sprintf(`(() => { const el=document.querySelector(%s); if(!el) return {found:false,visible:false}; const r=el.getBoundingClientRect(); let visible=r.width>0&&r.height>0; for(let cur=el; visible&&cur; cur=cur.parentElement) { const s=getComputedStyle(cur); visible=s.display!=='none'&&s.visibility!=='hidden'&&s.visibility!=='collapse'&&Number(s.opacity)!==0; } return {found:true,visible}; })()`, encoded)
}

func elementStateMatches(found, visible bool, state ElementState) bool {
	switch state {
	case StateAttached:
		return found
	case StateDetached:
		return !found
	case StateHidden:
		return !found || !visible
	default:
		return found && visible
	}
}

func urlMatches(current, expected string) bool {
	if !strings.ContainsAny(expected, "*?") {
		return strings.Contains(current, expected)
	}
	quoted := regexp.QuoteMeta(expected)
	quoted = strings.ReplaceAll(quoted, `\*`, `.*`)
	quoted = strings.ReplaceAll(quoted, `\?`, `.`)
	matched, _ := regexp.MatchString("^"+quoted+"$", current)
	return matched
}

func responseMatches(response responseRecord, action WaitResponseAction) bool {
	if action.URL != "" && !strings.Contains(response.URL, action.URL) {
		return false
	}
	if action.URLPattern != "" {
		matched, err := regexp.MatchString(action.URLPattern, response.URL)
		if err != nil || !matched {
			return false
		}
	}
	if action.Method != "" && !strings.EqualFold(action.Method, response.Method) {
		return false
	}
	return action.Status == 0 || action.Status == response.Status
}

func responseWaitDetails(action WaitResponseAction) *ErrorDetails {
	return &ErrorDetails{URL: action.URL, URLPattern: action.URLPattern, Method: action.Method, Status: action.Status}
}

func keyValue(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "enter", "return":
		return kb.Enter
	case "tab":
		return kb.Tab
	case "escape", "esc":
		return kb.Escape
	case "backspace":
		return kb.Backspace
	case "delete":
		return kb.Delete
	case "arrowup":
		return kb.ArrowUp
	case "arrowdown":
		return kb.ArrowDown
	case "arrowleft":
		return kb.ArrowLeft
	case "arrowright":
		return kb.ArrowRight
	default:
		return key
	}
}

func operationContext(parent, pageCtx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(pageCtx)
	if timeout > 0 {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, timeout)
		previous := cancel
		cancel = func() { timeoutCancel(); previous() }
	}
	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func withTimeout(parent context.Context, timeout, fallback time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = fallback
	}
	return context.WithTimeout(parent, timeout)
}

func wrapActionError(err error, index int, action string) error {
	if err == nil {
		return nil
	}
	var browserErr *Error
	if errors.As(err, &browserErr) {
		if browserErr.Details == nil {
			browserErr.Details = &ErrorDetails{}
		}
		browserErr.Details.ActionIndex = intPointer(index)
		browserErr.Details.Action = action
		return browserErr
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return browserError(ErrTimeout, "browser action timed out", "action", &ErrorDetails{ActionIndex: intPointer(index), Action: action}, err)
	}
	return browserError(ErrActionFailed, "browser action failed", "action", &ErrorDetails{ActionIndex: intPointer(index), Action: action}, err)
}

func intPointer(value int) *int { return &value }
