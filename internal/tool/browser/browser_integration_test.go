//go:build browser_integration

package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const integrationBrowserStartTimeout = 60 * time.Second

func integrationExecutable(t *testing.T) string {
	t.Helper()
	if path := strings.TrimSpace(os.Getenv("AGENTDOCK_BROWSER_EXECUTABLE_PATH")); path != "" {
		if _, err := FindExecutable(path, BrowserAuto); err != nil {
			t.Fatalf("browser integration explicitly configured executable is unusable: %v", err)
		}
		return path
	}
	found, err := FindExecutable("", BrowserAuto)
	if err != nil {
		t.Fatalf("browser_integration requires Chrome, Chromium, or Edge; integration tests must not skip: %v", err)
	}
	return found.Path
}

func integrationServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html>
<title>AgentDock Browser Integration</title>
<input id="name" value="initial">
<div id="fill-events"></div>
<select id="choice"><option value="one">one</option><option value="two">two</option></select>
<button id="fetch" onclick="fetch('/api?fast=1').then(()=>document.querySelector('#fetch-result').textContent='fetched')">fetch</button>
<button id="diagnostics" onclick="console.error('console-boom'); setTimeout(()=>{throw new Error('page-boom')},0); fetch('/network-boom').catch(()=>{})">diagnostics</button>
<button id="hide" onclick="document.querySelector('#hidden-parent').style.opacity='0'">hide</button>
<div id="hidden-parent"><div id="hidden">visible now</div></div>
<button id="remove" onclick="document.querySelector('#remove-me').remove()">remove</button>
<div id="remove-me">detach me</div>
<div id="fetch-result"></div>
<span id="alpha">Alpha</span><span id="beta">Beta</span>
<a id="popup" target="_blank" href="/popup">open popup</a>
<script>
document.body.dataset.cookie = document.cookie;
document.body.dataset.theme = localStorage.getItem('theme') || '';
const nameInput = document.querySelector('#name');
const fillEvents = document.querySelector('#fill-events');
nameInput.addEventListener('input', () => { fillEvents.textContent = 'input'; });
nameInput.addEventListener('change', () => { fillEvents.textContent += '-change'; });
</script>`)
	})
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/network-boom", func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking unavailable", http.StatusInternalServerError)
			return
		}
		conn, _, err := hijacker.Hijack()
		if err == nil {
			_ = conn.Close()
		}
	})
	mux.HandleFunc("/popup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<title>Popup</title><main id="popup-ready">popup-ready</main>`)
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loaded", http.StatusFound)
	})
	mux.HandleFunc("/loaded", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<title>Loaded</title><main id="loaded">redirect-loaded</main>`)
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintf(w, `<title>Slow</title><main>slow-page</main><img src="/slow-image?nonce=%s">`, r.URL.Query().Get("nonce"))
	})
	mux.HandleFunc("/slow-image", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		time.Sleep(450 * time.Millisecond)
		w.Header().Set("Content-Type", "image/gif")
		_, _ = w.Write([]byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;"))
	})
	mux.HandleFunc("/persist-set", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<title>Persist Set</title><script>localStorage.setItem('profile-key','profile-kept')</script><main>saved</main>`)
	})
	mux.HandleFunc("/persist-read", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<title>Persist Read</title><main id="persist"><script>document.write(localStorage.getItem('profile-key') || 'missing')</script></main>`)
	})
	return httptest.NewServer(mux)
}

func newIntegrationService(t *testing.T) *Service {
	t.Helper()
	service := New(Config{AgentDockHome: t.TempDir(), ExecutablePath: integrationExecutable(t)}, nil)
	t.Cleanup(func() { _ = service.Close() })
	return service
}

func startIntegrationSession(t *testing.T, service *Service, serverURL string, headless bool) StartResult {
	t.Helper()
	result, err := service.start(context.Background(), StartRequest{
		URL: serverURL, Browser: BrowserAuto, Headless: headless,
		Viewport: Viewport{Width: 1100, Height: 720}, Timeout: integrationBrowserStartTimeout,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return result
}

func TestBrowserIntegrationActionsSnapshotAndDiagnostics(t *testing.T) {
	server := integrationServer(t)
	defer server.Close()
	service := newIntegrationService(t)
	started := startIntegrationSession(t, service, server.URL, true)

	filled, err := service.act(context.Background(), ActRequest{
		SessionID: started.SessionID,
		Timeout:   5 * time.Second,
		Actions:   []Action{{Kind: "fill", Fill: &FillAction{Selector: "#name", Value: "event-check"}}},
	})
	if err != nil {
		t.Fatalf("fill behavior error = %v", err)
	}
	if filled.FocusedElement == nil || filled.FocusedElement.ID != "name" || !strings.Contains(filled.Text, "input-change") {
		t.Fatalf("fill behavior snapshot = %#v", filled)
	}

	snapshot, err := service.act(context.Background(), ActRequest{
		SessionID: started.SessionID,
		Timeout:   20 * time.Second,
		Actions: []Action{
			{Kind: "fill", Fill: &FillAction{Selector: "#name", Value: "updated"}},
			{Kind: "press", Press: &PressAction{Selector: "#name", Key: "End"}},
			{Kind: "select", Select: &SelectAction{Selector: "#choice", Value: "two"}},
			{Kind: "click", Click: &ClickAction{Selector: "#fetch"}},
			{Kind: "wait_for_response", WaitResponse: &WaitResponseAction{URL: "/api", Method: "GET", Status: 204, Timeout: 5 * time.Second}},
			{Kind: "wait_for_selector", WaitSelector: &WaitSelectorAction{Selector: "#fetch-result", State: StateVisible, Timeout: 5 * time.Second}},
			{Kind: "wait_for_text", WaitText: &WaitTextAction{Text: "fetched", Exact: true, State: StateVisible, Timeout: 5 * time.Second}},
			{Kind: "scroll", Scroll: &ScrollAction{DeltaY: 10}},
			{Kind: "click", Click: &ClickAction{Selector: "#hide"}},
			{Kind: "wait_for_selector", WaitSelector: &WaitSelectorAction{Selector: "#hidden", State: StateHidden, Timeout: 5 * time.Second}},
			{Kind: "wait_for_text", WaitText: &WaitTextAction{Text: "visible now", Exact: true, State: StateHidden, Timeout: 5 * time.Second}},
			{Kind: "click", Click: &ClickAction{Selector: "#remove"}},
			{Kind: "wait_for_text", WaitText: &WaitTextAction{Text: "detach me", Exact: true, State: StateDetached, Timeout: 5 * time.Second}},
			{Kind: "click", Click: &ClickAction{Selector: "#diagnostics"}},
			{Kind: "wait", Wait: &WaitAction{Duration: 300 * time.Millisecond}},
		},
		FullPage: true,
	})
	if err != nil {
		t.Fatalf("Act() error = %v", err)
	}
	if snapshot.SessionID != started.SessionID || snapshot.PageID == "" || snapshot.Title != "AgentDock Browser Integration" {
		t.Fatalf("snapshot identity = %#v", snapshot)
	}
	if len(snapshot.PNG) < 100 || snapshot.Viewport.Width <= 0 || snapshot.PageSize.Height <= 0 {
		t.Fatalf("snapshot image/layout invalid: png=%d viewport=%#v page=%#v", len(snapshot.PNG), snapshot.Viewport, snapshot.PageSize)
	}
	if len(snapshot.InteractiveElements) == 0 {
		t.Fatal("expected visible interactive elements")
	}
	_, err = service.snapshot(context.Background(), SnapshotRequest{SessionID: started.SessionID, PageID: "missing-target", Timeout: time.Second})
	var pageErr *Error
	if !errors.As(err, &pageErr) || pageErr.Code != ErrPageNotFound {
		t.Fatalf("missing page error = %v, want %s", err, ErrPageNotFound)
	}
	if !containsConsoleError(snapshot.ConsoleErrors, "console-boom") {
		t.Fatalf("console errors = %#v", snapshot.ConsoleErrors)
	}
	if !containsPageError(snapshot.PageErrors, "page-boom") {
		t.Fatalf("page errors = %#v", snapshot.PageErrors)
	}
	if !containsNetworkError(snapshot.NetworkErrors, "network-boom") {
		t.Fatalf("network errors = %#v", snapshot.NetworkErrors)
	}
}

func TestBrowserIntegrationExactElementTextAndNavigationLifecycle(t *testing.T) {
	server := integrationServer(t)
	defer server.Close()
	service := newIntegrationService(t)
	started := startIntegrationSession(t, service, server.URL, true)

	_, err := service.act(context.Background(), ActRequest{
		SessionID: started.SessionID, Timeout: 5 * time.Second,
		Actions: []Action{{Kind: "wait_for_text", WaitText: &WaitTextAction{Text: "Alpha", Exact: true, State: StateVisible, Timeout: time.Second}}},
	})
	if err != nil {
		t.Fatalf("exact element text should match: %v", err)
	}

	_, err = service.act(context.Background(), ActRequest{
		SessionID: started.SessionID, Timeout: 3 * time.Second,
		Actions: []Action{{Kind: "select", Select: &SelectAction{Selector: "#choice", Value: "missing-option"}}},
	})
	var actionErr *Error
	if !errors.As(err, &actionErr) || actionErr.Code != ErrActionFailed {
		t.Fatalf("missing option error = %v, want %s", err, ErrActionFailed)
	}
	if _, err := service.snapshot(context.Background(), SnapshotRequest{SessionID: started.SessionID, Timeout: 5 * time.Second}); err != nil {
		t.Fatalf("session was not retained after action failure: %v", err)
	}
	_, err = service.act(context.Background(), ActRequest{
		SessionID: started.SessionID, Timeout: 3 * time.Second,
		Actions: []Action{{Kind: "wait_for_text", WaitText: &WaitTextAction{Text: "Alpha Beta", Exact: true, State: StateVisible, Timeout: 250 * time.Millisecond}}},
	})
	var browserErr *Error
	if !errors.As(err, &browserErr) || browserErr.Code != ErrTimeout {
		t.Fatalf("whole-document-only exact text error = %v, want timeout", err)
	}

	startedAt := time.Now()
	_, err = service.act(context.Background(), ActRequest{
		SessionID: started.SessionID, Timeout: 5 * time.Second,
		Actions: []Action{{Kind: "goto", Goto: &GotoAction{URL: server.URL + "/slow?nonce=load", WaitUntil: WaitLoad, Timeout: 3 * time.Second}}},
	})
	if err != nil {
		t.Fatalf("load navigation error = %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed < 400*time.Millisecond {
		t.Fatalf("wait_until=load returned before slow resource loaded: %v", elapsed)
	}

	snapshot, err := service.act(context.Background(), ActRequest{
		SessionID: started.SessionID, Timeout: 5 * time.Second,
		Actions: []Action{{Kind: "goto", Goto: &GotoAction{URL: server.URL + "/redirect", WaitUntil: WaitDOMContentLoaded, Timeout: 3 * time.Second}}, {Kind: "wait_for_url", WaitURL: &WaitURLAction{URL: "/loaded", Timeout: time.Second}}},
	})
	if err != nil || !strings.Contains(snapshot.URL, "/loaded") || !strings.Contains(snapshot.Text, "redirect-loaded") {
		t.Fatalf("redirect navigation = url:%q text:%q err:%v", snapshot.URL, snapshot.Text, err)
	}

	back, err := service.act(context.Background(), ActRequest{SessionID: started.SessionID, Timeout: 5 * time.Second, Actions: []Action{{Kind: "back", Navigation: &NavigationAction{WaitUntil: WaitLoad, Timeout: 3 * time.Second}}}})
	if err != nil || !strings.Contains(back.URL, "/slow") {
		t.Fatalf("back navigation = %q err=%v", back.URL, err)
	}
	forward, err := service.act(context.Background(), ActRequest{SessionID: started.SessionID, Timeout: 5 * time.Second, Actions: []Action{{Kind: "forward", Navigation: &NavigationAction{WaitUntil: WaitLoad, Timeout: 3 * time.Second}}, {Kind: "reload", Navigation: &NavigationAction{WaitUntil: WaitLoad, Timeout: 3 * time.Second}}}})
	if err != nil || !strings.Contains(forward.URL, "/loaded") {
		t.Fatalf("forward/reload navigation = %q err=%v", forward.URL, err)
	}
}

func TestBrowserIntegrationInjectionProfilesTargetsAndCleanup(t *testing.T) {
	server := integrationServer(t)
	defer server.Close()
	service := newIntegrationService(t)

	started, err := service.start(context.Background(), StartRequest{
		URL: server.URL, Browser: BrowserAuto, Headless: true, Timeout: integrationBrowserStartTimeout,
		Viewport:     Viewport{Width: 900, Height: 600},
		Cookies:      []Cookie{{Name: "agentdock_cookie", Value: "cookie-ok", URL: server.URL}},
		LocalStorage: map[string]map[string]string{server.URL: {"theme": "dark"}}, ReloadAfterLocalStorage: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := service.getSession(started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	tempProfile := sess.profileDir
	if !sess.temporaryProfile {
		t.Fatal("session without profile_id should use a temporary profile")
	}
	if _, err := os.Stat(tempProfile); err != nil {
		t.Fatalf("temporary profile missing while session is active: %v", err)
	}

	var state struct {
		Cookie string `json:"cookie"`
		Theme  string `json:"theme"`
	}
	pageID, _ := sess.selectPage("")
	pageCtx, err := sess.ensurePageContext(context.Background(), pageID)
	if err != nil {
		t.Fatal(err)
	}
	if err := runWithContext(context.Background(), pageCtx, chromedp.Evaluate(`({cookie:document.cookie,theme:localStorage.getItem('theme')})`, &state)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.Cookie, "agentdock_cookie=cookie-ok") || state.Theme != "dark" {
		t.Fatalf("injected state = %#v", state)
	}

	popup, err := service.act(context.Background(), ActRequest{SessionID: started.SessionID, Timeout: 10 * time.Second, Actions: []Action{{Kind: "click", Click: &ClickAction{Selector: "#popup"}}, {Kind: "wait_for_text", WaitText: &WaitTextAction{Text: "popup-ready", Exact: true, State: StateVisible, Timeout: 5 * time.Second}}}})
	if err != nil {
		var browserErr *Error
		if errors.As(err, &browserErr) {
			t.Fatalf("new target action error = %v phase=%s details=%#v", err, browserErr.Phase, browserErr.Details)
		}
		t.Fatalf("new target action error = %v", err)
	}
	if popup.PageID == started.PageID || popup.Title != "Popup" || len(popup.Pages) < 2 {
		t.Fatalf("new target did not become active: start=%s snapshot=%#v", started.PageID, popup)
	}

	pinned, err := service.act(context.Background(), ActRequest{
		SessionID: started.SessionID,
		PageID:    started.PageID,
		Timeout:   10 * time.Second,
		Actions: []Action{
			{Kind: "fill", Fill: &FillAction{Selector: "#name", Value: "pinned-page"}},
			{Kind: "wait_for_text", WaitText: &WaitTextAction{Text: "input-change", Exact: true, State: StateVisible, Timeout: 5 * time.Second}},
		},
	})
	if err != nil {
		t.Fatalf("explicit page_id drifted during multi-action request: %v", err)
	}
	if pinned.PageID != started.PageID || pinned.Title != "AgentDock Browser Integration" {
		t.Fatalf("explicit page_id result = %#v, want page %s", pinned, started.PageID)
	}

	popupSess, _ := service.getSession(started.SessionID)
	popupPage, _ := popupSess.selectPage(popup.PageID)
	popupSess.mu.Lock()
	cachedPopup := popupSess.pageContexts[popupPage]
	popupSess.mu.Unlock()
	if cachedPopup == nil {
		t.Fatal("popup page context was not cached")
	}
	cachedPopup.cancel()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := popupSess.refreshPages(); err == nil {
			active, _ := popupSess.selectPage("")
			if string(active) == started.PageID {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	active, err := popupSess.selectPage("")
	if err != nil || string(active) != started.PageID {
		t.Fatalf("active page after popup close = %q err=%v, want %q", active, err, started.PageID)
	}

	if _, err := service.closeSession(CloseRequest{SessionID: started.SessionID}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tempProfile); !errors.Is(err, os.ErrNotExist) {
		entries, _ := os.ReadDir(tempProfile)
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("temporary profile was not removed: %v path=%s entries=%v", err, tempProfile, names)
	}

	blank, err := service.start(context.Background(), StartRequest{
		Browser: BrowserAuto, Headless: true, Timeout: integrationBrowserStartTimeout,
		LocalStorage: map[string]map[string]string{server.URL: {"profile-key": "profile-kept"}},
	})
	if err != nil {
		t.Fatalf("start with localStorage and default URL: %v", err)
	}
	if blank.URL != "about:blank" {
		t.Fatalf("default URL after localStorage injection = %q, want about:blank", blank.URL)
	}
	var blankPageURL string
	for _, page := range blank.Pages {
		if page.PageID == blank.PageID {
			blankPageURL = page.URL
			break
		}
	}
	if blankPageURL != "about:blank" {
		t.Fatalf("active page URL after localStorage injection = %q, want about:blank", blankPageURL)
	}
	injected, err := service.act(context.Background(), ActRequest{
		SessionID: blank.SessionID,
		Timeout:   10 * time.Second,
		Actions: []Action{
			{Kind: "goto", Goto: &GotoAction{URL: server.URL + "/persist-read", WaitUntil: WaitLoad, Timeout: 5 * time.Second}},
		},
	})
	if err != nil || !strings.Contains(injected.Text, "profile-kept") {
		t.Fatalf("localStorage after restoring about:blank = %q err=%v", injected.Text, err)
	}
	if _, err := service.closeSession(CloseRequest{SessionID: blank.SessionID}); err != nil {
		t.Fatal(err)
	}

	profile := "integration-persist"
	first, err := service.start(context.Background(), StartRequest{URL: server.URL + "/persist-set", Browser: BrowserAuto, Headless: true, ProfileID: profile, Timeout: integrationBrowserStartTimeout})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.start(context.Background(), StartRequest{URL: server.URL, Browser: BrowserAuto, Headless: true, ProfileID: profile, Timeout: 5 * time.Second}); err == nil {
		t.Fatal("same profile started twice")
	} else {
		var profileErr *Error
		if !errors.As(err, &profileErr) || profileErr.Code != ErrProfileInUse {
			t.Fatalf("duplicate profile error = %v", err)
		}
	}
	if _, err := service.closeSession(CloseRequest{SessionID: first.SessionID}); err != nil {
		t.Fatal(err)
	}
	second, err := service.start(context.Background(), StartRequest{URL: server.URL + "/persist-read", Browser: BrowserAuto, Headless: true, ProfileID: profile, Timeout: integrationBrowserStartTimeout})
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := service.snapshot(context.Background(), SnapshotRequest{SessionID: second.SessionID, Timeout: 10 * time.Second})
	if err != nil || !strings.Contains(persisted.Text, "profile-kept") {
		t.Fatalf("persistent profile snapshot = %q err=%v", persisted.Text, err)
	}
}

func TestBrowserIntegrationVisibleModeAndShutdownOwnsProcess(t *testing.T) {
	server := integrationServer(t)
	defer server.Close()
	service := newIntegrationService(t)
	started := startIntegrationSession(t, service, server.URL, false)
	sess, err := service.getSession(started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	ctx := chromedp.FromContext(sess.browserCtx)
	if ctx == nil || ctx.Browser == nil || ctx.Browser.Process() == nil {
		t.Fatal("AgentDock-launched browser process is unavailable")
	}
	process := ctx.Browser.Process()
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := process.Signal(syscall.Signal(0)); err != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("browser process %d is still alive after Service.Close()", process.Pid)
	}
}

func containsConsoleError(values []ConsoleError, substring string) bool {
	for _, value := range values {
		if strings.Contains(value.Message, substring) {
			return true
		}
	}
	return false
}

func containsPageError(values []PageError, substring string) bool {
	for _, value := range values {
		if strings.Contains(value.Message, substring) {
			return true
		}
	}
	return false
}

func containsNetworkError(values []NetworkError, substring string) bool {
	for _, value := range values {
		if strings.Contains(value.URL, substring) || strings.Contains(value.ErrorText, substring) {
			return true
		}
	}
	return false
}
