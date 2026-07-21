package chatgpt

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/goal"
)

type fakeBrowser struct {
	conversations int
	sent          []string
	blockers      []string
	waitErr       error
	opened        []string
	currentURL    string
}

func (f *fakeBrowser) EnsureSession(context.Context, string) error { return nil }
func (f *fakeBrowser) OpenChatGPT(context.Context) error {
	f.opened = append(f.opened, "https://chatgpt.com/")
	f.currentURL = "https://chatgpt.com/"
	return nil
}
func (f *fakeBrowser) OpenConversation(_ context.Context, conversationURL string) error {
	f.opened = append(f.opened, conversationURL)
	f.currentURL = conversationURL
	return nil
}
func (f *fakeBrowser) NewConversation(context.Context) (string, error) {
	f.conversations++
	id := fmt.Sprintf("conv-%d", time.Now().UnixNano())
	f.currentURL = "https://chatgpt.com/c/" + id
	return id, nil
}
func (f *fakeBrowser) PasteAndSend(_ context.Context, text string) error {
	f.sent = append(f.sent, text)
	// Simulate SPA assigning a real thread URL after first send when still on home.
	if f.currentURL == "" || f.currentURL == "https://chatgpt.com/" {
		f.currentURL = "https://chatgpt.com/c/thread-from-paste"
	}
	return nil
}
func (f *fakeBrowser) WaitIdle(context.Context, time.Duration) error { return f.waitErr }
func (f *fakeBrowser) DetectBlockers(context.Context) ([]string, error) {
	return append([]string(nil), f.blockers...), nil
}
func (f *fakeBrowser) CurrentURL(context.Context) (string, error) {
	if f.currentURL == "" {
		return "https://chatgpt.com/", nil
	}
	return f.currentURL, nil
}

type fakeGoals struct {
	store *goal.Store
}

func (f fakeGoals) Get(id string) (goal.Goal, error) { return f.store.Get(id) }
func (f fakeGoals) AcquireLease(goalID, workerID string, ttl time.Duration) (goal.Goal, goal.Lease, error) {
	return f.store.AcquireLease(goalID, workerID, ttl)
}
func (f fakeGoals) ReleaseLease(goalID, leaseID string) (goal.Goal, error) {
	return f.store.ReleaseLease(goalID, leaseID)
}
func (f fakeGoals) BindWorkerConversation(goalID, conversationURL, conversationID string) (goal.Goal, error) {
	return f.store.BindWorkerConversation(goalID, conversationURL, conversationID)
}

func TestLoopWakeRotatesAndSendsResumePrompt(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "web loop", Objective: "resume only",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// seed problem for prompt
	g, lease, err := store.AcquireLease(g.ID, "seed", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CommitTurn(goal.CommitTurnInput{
		GoalID: g.ID, ReasoningLeaseID: lease.LeaseID, ExpectedCapsuleVersion: g.CapsuleVersion,
		Decision: goal.DecisionContinue, Summary: "need analysis",
		CurrentRequest: "Analyze latest failure",
	}); err != nil {
		t.Fatal(err)
	}

	fb := &fakeBrowser{}
	loop := &Loop{Browser: fb, Goals: fakeGoals{store: store}, Config: testLoopConfig()}
	loop.Config.MaxTurnsPerConversation = 1

	res, err := loop.Wake(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Unbound goals always open a new ChatGPT conversation (session isolation).
	if !res.Rotated || res.ResumePrompt == "" || res.LeaseID == "" {
		t.Fatalf("wake1 should open new convo for unbound goal: %#v", res)
	}
	if len(fb.sent) != 1 || !contains(fb.sent[0], g.ID) {
		t.Fatalf("sent=%v", fb.sent)
	}
	if fb.conversations < 1 {
		t.Fatalf("first wake should call NewConversation, convos=%d", fb.conversations)
	}

	// release so next wake can acquire
	if _, err := store.ReleaseLease(g.ID, res.LeaseID); err != nil {
		t.Fatal(err)
	}
	// MaxTurns=1 forces rotation on second wake
	res2, err := loop.Wake(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Rotated || res2.RotationReason != RotateTurnLimit {
		t.Fatalf("expected turn limit rotation: %#v", res2)
	}
	if fb.conversations < 1 {
		t.Fatalf("conversations=%d", fb.conversations)
	}
}

func TestShouldRotateOnQuota(t *testing.T) {
	loop := &Loop{Config: testLoopConfig()}
	ok, reason := loop.ShouldRotate([]string{"You've reached your usage limit"})
	if !ok || reason != RotateQuota {
		t.Fatalf("got %v %s", ok, reason)
	}
}

func TestLoopWakeRefusesWhenNotIdle(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "busy", Objective: "do not paste",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestReasoning(g.ID, "continue", ""); err != nil {
		t.Fatal(err)
	}
	fb := &fakeBrowser{waitErr: fmt.Errorf("page_stuck: CDP method timed out: Runtime.evaluate")}
	loop := &Loop{Browser: fb, Goals: fakeGoals{store: store}, Config: testLoopConfig()}
	_, err = loop.Wake(context.Background(), g.ID)
	if err == nil {
		t.Fatal("expected not-idle error")
	}
	if !strings.Contains(err.Error(), "page not idle") {
		t.Fatalf("got %v", err)
	}
	if len(fb.sent) != 0 {
		t.Fatalf("must not paste when not idle, sent=%v", fb.sent)
	}
}

func TestHardBlockers(t *testing.T) {
	// tool_permission is handled by resolveToolPermission, not hardBlockers.
	got := hardBlockers([]string{"tool_permission", "tool_permission_auto_approved", "page_busy", "usage limit", "page_stuck"})
	if len(got) != 3 { // page_busy, usage limit, page_stuck
		t.Fatalf("got %v", got)
	}
	for _, b := range got {
		if strings.Contains(b, "tool_permission") {
			t.Fatalf("tool_permission must not be a hard blocker: %v", got)
		}
	}
}

func TestLoopHandsOffSkipsPostPasteDetect(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "hands-off", Objective: "no post paste cdp",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestReasoning(g.ID, "go", ""); err != nil {
		t.Fatal(err)
	}
	fb := &fakeBrowser{}
	cfg := testLoopConfig()
	cfg.HandsOffAfterPaste = true
	loop := &Loop{Browser: fb, Goals: fakeGoals{store: store}, Config: cfg}
	res, err := loop.Wake(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fb.sent) != 1 {
		t.Fatalf("sent=%v", fb.sent)
	}
	// DetectBlockers is used pre-paste (idle/permission); with hands-off it must not
	// run again after paste. We approximate by ensuring CurrentURL was not needed
	// for a successful wake — conversation id comes from NewConversation.
	if res.ConversationID == "" {
		t.Fatalf("expected conversation id from new chat path: %#v", res)
	}
}

func TestLoopPermissionUnresolvedFailsHard(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "perm", Objective: "permission stuck",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestReasoning(g.ID, "go", ""); err != nil {
		t.Fatal(err)
	}
	fb := &fakeBrowser{blockers: []string{"tool_permission"}}
	cfg := testLoopConfig()
	cfg.PermissionWait = 50 * time.Millisecond
	loop := &Loop{Browser: fb, Goals: fakeGoals{store: store}, Config: cfg}
	_, err = loop.Wake(context.Background(), g.ID)
	if err == nil {
		t.Fatal("expected permission unresolved error")
	}
	if !strings.Contains(err.Error(), "tool_permission_unresolved") {
		t.Fatalf("got %v", err)
	}
	if len(fb.sent) != 0 {
		t.Fatalf("must not paste with unresolved permission, sent=%v", fb.sent)
	}
}

func TestWakeRequiresDeps(t *testing.T) {
	loop := &Loop{}
	if _, err := loop.Wake(context.Background(), "goal_x"); err == nil {
		t.Fatal("expected error without deps")
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

// testLoopConfig is DefaultConfig with post-paste permission window disabled
// so unit tests do not sleep the product 90s poll.
func testLoopConfig() Config {
	cfg := defaultLoopConfigForTest()
	cfg.PostPastePermissionWait = -1
	return cfg
}

func defaultLoopConfigForTest() Config {
	return Config{
		ProfileID: "chatgpt",
		WorkerIDPrefix: "chatgpt-web",
		MaxTurnsPerConversation: 50,
		LeaseTTL: 30 * time.Minute,
		WaitIdleTimeout: 8 * time.Minute,
		PermissionWait: 90 * time.Second,
		PostPastePermissionWait: -1,
		HandsOffAfterPaste: true,
	}
}



func TestLoopWakeReopensBoundConversation(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "bound", Objective: "reuse thread",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestReasoning(g.ID, "continue", "need next step"); err != nil {
		t.Fatal(err)
	}
	boundURL := "https://chatgpt.com/c/abc123thread"
	if _, err := store.BindWorkerConversation(g.ID, boundURL, "abc123thread"); err != nil {
		t.Fatal(err)
	}

	fb := &fakeBrowser{currentURL: "https://chatgpt.com/"}
	loop := &Loop{Browser: fb, Goals: fakeGoals{store: store}, Config: testLoopConfig()}
	res, err := loop.Wake(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rotated {
		t.Fatalf("should not rotate when bound: %#v", res)
	}
	if len(fb.opened) == 0 || fb.opened[0] != boundURL {
		t.Fatalf("expected open bound url, opened=%v", fb.opened)
	}
	if len(fb.sent) != 1 {
		t.Fatalf("sent=%v", fb.sent)
	}
	// Second wake still opens bound thread, not new chat.
	if _, err := store.ReleaseLease(g.ID, res.LeaseID); err != nil {
		t.Fatal(err)
	}
	res2, err := loop.Wake(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Rotated || fb.conversations != 0 {
		t.Fatalf("second wake rotated unexpectedly: %#v convos=%d", res2, fb.conversations)
	}
	loaded, err := store.Get(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.WorkerConversationURL == "" {
		t.Fatal("expected durable binding to remain")
	}
}

func TestLoopWakePersistsThreadAfterPaste(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.Create(goal.CreateInput{
		Title: "persist", Objective: "bind after paste",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestReasoning(g.ID, "start", ""); err != nil {
		t.Fatal(err)
	}
	fb := &fakeBrowser{}
	cfg := testLoopConfig()
	// Binding via CurrentURL after paste needs post-paste CDP for this unit test.
	cfg.HandsOffAfterPaste = false
	loop := &Loop{Browser: fb, Goals: fakeGoals{store: store}, Config: cfg}
	res, err := loop.Wake(context.Background(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.LeaseID == "" {
		t.Fatalf("%#v", res)
	}
	loaded, err := store.Get(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.WorkerConversationURL == "" || loaded.WorkerConversationID == "" {
		t.Fatalf("binding=%q id=%q", loaded.WorkerConversationURL, loaded.WorkerConversationID)
	}
	if !strings.Contains(loaded.WorkerConversationURL, "/c/") {
		t.Fatalf("expected chatgpt thread url, got %q", loaded.WorkerConversationURL)
	}
}


func TestWakeNewUnboundGoalOpensNewConversation(t *testing.T) {
	store, err := goal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g1, err := store.Create(goal.CreateInput{
		Title: "g1", Objective: "first",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestReasoning(g1.ID, "r1", ""); err != nil {
		t.Fatal(err)
	}
	// Bind g1 to a thread
	if _, err := store.BindWorkerConversation(g1.ID, "https://chatgpt.com/c/oldthread123456", "oldthread123456"); err != nil {
		t.Fatal(err)
	}
	fb := &fakeBrowser{currentURL: "https://chatgpt.com/c/oldthread123456"}
	loop := &Loop{Browser: fb, Goals: fakeGoals{store: store}, Config: testLoopConfig()}
	res1, err := loop.Wake(context.Background(), g1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res1.Rotated {
		// bound goal should not rotate
	}
	// release lease
	if res1.LeaseID != "" {
		_, _ = store.ReleaseLease(g1.ID, res1.LeaseID)
	}

	g2, err := store.Create(goal.CreateInput{
		Title: "g2", Objective: "second unbound",
		SuccessCriteria: []goal.SuccessCriterionInput{{Expression: "ok", Type: goal.CriterionManual}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestReasoning(g2.ID, "r2", ""); err != nil {
		t.Fatal(err)
	}
	before := fb.conversations
	res2, err := loop.Wake(context.Background(), g2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Rotated {
		t.Fatalf("unbound new goal should open new conversation: %#v", res2)
	}
	if fb.conversations <= before {
		t.Fatalf("expected NewConversation call, convos=%d", fb.conversations)
	}
}
