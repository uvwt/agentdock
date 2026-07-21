package chatgpt

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/goal"
)

type fakeCaller struct {
	calls []string
}

func (f *fakeCaller) Call(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, name)
	switch name {
	case "browser_session":
		return map[string]any{"page_id": "page-1", "browser_ok": true}, nil
	case "browser_act":
		// pageBusy / evaluate probes expect a structured result payload.
		return map[string]any{
			"browser_ok": true,
			"page_id":    "page-1",
			"result":     map[string]any{"busy": false, "streaming": false, "tools": false, "permission": false},
		}, nil
	case "browser_snapshot":
		return map[string]any{"browser_ok": true, "page_id": "page-1", "text": "composer ready", "url": "https://chatgpt.com/"}, nil
	default:
		return map[string]any{}, nil
	}
}

func TestWorkerWakeWithFakeBrowser(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "wake me", Objective: "need reasoning",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestReasoning(g.ID, "analyze failure", "POST /login 500"); err != nil {
		t.Fatal(err)
	}

	caller := &fakeCaller{}
	w := NewWorker(store, caller, WorkerOptions{
		ProfileID: "chatgpt", Headless: true, AutoWake: false, BrowserEnabled: true,
		MaxTurnsPerConversation: 2, WaitIdleTimeout: time.Second, LeaseTTL: time.Minute,
	})
	res, err := w.Wake(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res["ok"] != true {
		t.Fatalf("res=%#v", res)
	}
	if res["lease_id"] == "" || res["resume_prompt"] == "" {
		t.Fatalf("missing lease/prompt: %#v", res)
	}
	joined := strings.Join(caller.calls, ",")
	if !strings.Contains(joined, "browser_session") || !strings.Contains(joined, "browser_act") {
		t.Fatalf("browser not used: %s", joined)
	}

	// second wake should rotate after turn limit eventually
	w.ForceRotate()
	if _, err := store.ReleaseLease(g.ID, res["lease_id"].(string)); err != nil {
		// lease may already be held until commit - release for second wake with same worker path
	}
	// Acquire may conflict if previous lease still active - release via store if present
	if loaded, err := store.Get(g.ID); err == nil && loaded.ActiveLease != nil {
		_, _ = store.ReleaseLease(g.ID, loaded.ActiveLease.LeaseID)
	}
	res2, err := w.Wake(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res2["ok"] != true {
		t.Fatalf("res2=%#v", res2)
	}
}

func TestMaybeAutoWakeOnlyAwaitingReasoning(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "x", Objective: "y",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorker(store, &fakeCaller{}, WorkerOptions{BrowserEnabled: true, AutoWake: true, Headless: true, WaitIdleTimeout: time.Millisecond * 50})
	// planning should not panic
	w.MaybeAutoWake(g)
	g.Status = goal.StatusAwaitingReasoning
	w.MaybeAutoWake(g)
	time.Sleep(20 * time.Millisecond)
	st := w.Status()
	if st["auto_wake"] != true {
		t.Fatalf("%#v", st)
	}
}

func TestRuntimeBrowserRebindsStalePageID(t *testing.T) {
	var calls []map[string]any
	caller := &seqCaller{handlers: []func(name string, args map[string]any) (map[string]any, error){
		// browser_session start
		func(name string, args map[string]any) (map[string]any, error) {
			if name != "browser_session" {
				t.Fatalf("expected session, got %s", name)
			}
			return map[string]any{"session_id": "sess-1", "page_id": "page-1"}, nil
		},
	}}
	// Remaining browser_act calls: stale page-1 fails once, then rebind succeeds.
	// PasteAndSend may issue multiple acts (fill + click/press).
	caller.dynamic = func(name string, args map[string]any) (map[string]any, error) {
		if name == "browser_snapshot" {
			return map[string]any{"browser_ok": true, "page_id": "page-9", "text": "ok"}, nil
		}
		if name != "browser_act" {
			return map[string]any{"browser_ok": true}, nil
		}
		calls = append(calls, args)
		if args["page_id"] == "page-1" {
			return map[string]any{"browser_ok": false, "browser_error": "Unknown browser page_id: page-1"}, nil
		}
		return map[string]any{"browser_ok": true, "page_id": "page-9"}, nil
	}
	b := &RuntimeBrowser{Caller: caller, ProfileID: "chatgpt", Headless: true}
	if err := b.EnsureSession(context.Background(), "chatgpt"); err != nil {
		t.Fatal(err)
	}
	if b.pageID != "page-1" {
		t.Fatalf("page=%s", b.pageID)
	}
	// OpenChatGPT now no-ops when a page binding exists (to preserve the active chat).
	// Exercise stale-page rebind via PasteAndSend, which still calls browser_act.
	if err := b.PasteAndSend(context.Background(), "resume prompt"); err != nil {
		t.Fatal(err)
	}
	if b.pageID != "page-9" {
		t.Fatalf("expected rebound page-9, got %s", b.pageID)
	}
	if len(calls) < 2 {
		t.Fatalf("expected fail+retry acts, got %d", len(calls))
	}
}

func TestWorkerBusyWakeReturnsError(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorker(store, &fakeCaller{}, WorkerOptions{BrowserEnabled: true, AutoWake: false, Headless: true})
	w.waking = true
	res, err := w.Wake(context.Background(), "goal_x")
	if err == nil {
		t.Fatalf("expected busy error, res=%#v", res)
	}
	if res["busy"] != true {
		t.Fatalf("%#v", res)
	}
}

type seqCaller struct {
	i        int
	handlers []func(name string, args map[string]any) (map[string]any, error)
	dynamic  func(name string, args map[string]any) (map[string]any, error)
}

func (s *seqCaller) Call(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	if s.i < len(s.handlers) {
		h := s.handlers[s.i]
		s.i++
		return h(name, args)
	}
	if s.dynamic != nil {
		return s.dynamic(name, args)
	}
	return map[string]any{"browser_ok": true}, nil
}


func TestWakeCooldownSkipsRepaste(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "cd", Objective: "cooldown",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestReasoning(g.ID, "r", "p"); err != nil {
		t.Fatal(err)
	}
	w := NewWorker(store, &fakeCaller{}, WorkerOptions{
		BrowserEnabled: true, AutoWake: false, Headless: true,
		WaitIdleTimeout: time.Millisecond * 20, WakeCooldown: time.Minute,
	})
	res1, err := w.Wake(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res1["ok"] != true || res1["skipped"] == true {
		t.Fatalf("first wake %#v", res1)
	}
	// release lease so second path could acquire; cooldown should skip before browser
	if lid, _ := res1["lease_id"].(string); lid != "" {
		_, _ = store.ReleaseLease(g.ID, lid)
	}
	res2, err := w.Wake(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res2["skipped"] != true || res2["cooldown"] != true {
		t.Fatalf("expected cooldown skip %#v", res2)
	}
	w.ClearWakeCooldown(g.ID)
	// after clear, may still fail lease if active; release again
	if loaded, err := store.Get(g.ID); err == nil && loaded.ActiveLease != nil {
		_, _ = store.ReleaseLease(g.ID, loaded.ActiveLease.LeaseID)
	}
	res3, err := w.Wake(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res3["skipped"] == true {
		t.Fatalf("after clear should paste %#v", res3)
	}
}

func TestMaybeAutoWakeSuppressedByOrchestrator(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "s", Objective: "suppress",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	g.Status = goal.StatusAwaitingReasoning
	caller := &fakeCaller{}
	w := NewWorker(store, caller, WorkerOptions{BrowserEnabled: true, AutoWake: true, Headless: true, WaitIdleTimeout: time.Millisecond * 20, WakeCooldown: -1})
	w.SetAutoWakeSuppressed(g.ID, true)
	w.MaybeAutoWake(g)
	time.Sleep(30 * time.Millisecond)
	if len(caller.calls) != 0 {
		t.Fatalf("suppressed auto-wake still called browser: %v", caller.calls)
	}
	w.SetAutoWakeSuppressed(g.ID, false)
	w.MaybeAutoWake(g)
	time.Sleep(80 * time.Millisecond)
	if len(caller.calls) == 0 {
		t.Fatal("expected auto-wake after unsuppress")
	}
}


func TestAutoApproveToolsDefaultOff(t *testing.T) {
	w := NewWorker(nil, &fakeCaller{}, DefaultWorkerOptions())
	if w.AutoApproveToolsEnabled() {
		t.Fatal("default auto approve should be off")
	}
	w.SetAutoApproveTools(true)
	if !w.AutoApproveToolsEnabled() || w.browser == nil || !w.browser.AutoApproveTools {
		t.Fatalf("expected enabled on worker+browser")
	}
	w.SetAutoApproveTools(false)
	if w.AutoApproveToolsEnabled() || w.browser.AutoApproveTools {
		t.Fatal("expected disabled")
	}
}


func TestActDoesNotThrashOnInvalidJSON(t *testing.T) {
	var n int
	caller := &seqCaller{dynamic: func(name string, args map[string]any) (map[string]any, error) {
		if name == "browser_session" {
			return map[string]any{"session_id": "s1", "page_id": "p1", "browser_ok": true}, nil
		}
		if name != "browser_act" {
			return map[string]any{"browser_ok": true}, nil
		}
		n++
		return map[string]any{"browser_ok": false, "browser_error": "browser runner returned invalid JSON"}, nil
	}}
	b := &RuntimeBrowser{Caller: caller, ProfileID: "chatgpt", Headless: true}
	if err := b.EnsureSession(context.Background(), "chatgpt"); err != nil {
		t.Fatal(err)
	}
	err := b.act(context.Background(), []map[string]any{{"action": "goto", "url": "https://chatgpt.com/"}})
	if err == nil {
		t.Fatal("expected invalid JSON to fail without rebind storm")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("got %v", err)
	}
	// Only one attempt — no EnsureSession/OpenChatGPT cascade.
	if n != 1 {
		t.Fatalf("expected single act attempt, n=%d", n)
	}
}
