package browser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/target"
)

func TestFindExecutableUsesConfiguredPathAndRejectsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chromium")
	if err := os.WriteFile(path, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}

	found, err := FindExecutable(path, BrowserChromium)
	if err != nil {
		t.Fatalf("FindExecutable() error = %v", err)
	}
	if found.Path != path || found.Kind != BrowserChromium {
		t.Fatalf("FindExecutable() = %#v", found)
	}

	_, err = FindExecutable(filepath.Join(t.TempDir(), "missing"), BrowserChrome)
	var browserErr *Error
	if !errors.As(err, &browserErr) || browserErr.Code != ErrNotFound || browserErr.Phase != "discovery" {
		t.Fatalf("missing executable error = %#v, want %s/discovery", err, ErrNotFound)
	}
}

func TestExternalCDPRejectsProfileMutationsBeforeConnecting(t *testing.T) {
	service := New(Config{AgentDockHome: t.TempDir()}, nil)
	for _, req := range []StartRequest{
		{CDPURL: "http://127.0.0.1:9", Cookies: []Cookie{{Name: "session", Value: "value"}}},
		{CDPURL: "http://127.0.0.1:9", LocalStorage: map[string]map[string]string{"https://example.test": {"key": "value"}}},
	} {
		_, err := service.start(context.Background(), req)
		var browserErr *Error
		if !errors.As(err, &browserErr) || browserErr.Code != ErrActionInvalid {
			t.Fatalf("Start() error = %#v, want %s", err, ErrActionInvalid)
		}
	}
}

func TestBrowserErrorCodeContract(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "not found", got: ErrNotFound, want: "BROWSER_NOT_FOUND"},
		{name: "launch failed", got: ErrLaunchFailed, want: "LAUNCH_FAILED"},
		{name: "profile in use", got: ErrProfileInUse, want: "PROFILE_IN_USE"},
		{name: "session not found", got: ErrSessionNotFound, want: "SESSION_NOT_FOUND"},
		{name: "page not found", got: ErrPageNotFound, want: "PAGE_NOT_FOUND"},
		{name: "action invalid", got: ErrActionInvalid, want: "ACTION_INVALID"},
		{name: "action failed", got: ErrActionFailed, want: "ACTION_FAILED"},
		{name: "timeout", got: ErrTimeout, want: "TIMEOUT"},
		{name: "cdp failed", got: ErrCDPFailed, want: "CDP_FAILED"},
		{name: "cdp ambiguous", got: ErrCDPAmbiguous, want: "CDP_AMBIGUOUS"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("browser error code = %q, want %q", test.got, test.want)
			}
		})
	}
}

func TestExecutableCandidatesRespectRequestedBrowser(t *testing.T) {
	candidates := executableCandidates("darwin", BrowserEdge)
	if len(candidates) == 0 {
		t.Fatal("expected Edge candidates")
	}
	for _, candidate := range candidates {
		if candidate.Kind != BrowserEdge {
			t.Fatalf("candidate kind = %q, want edge", candidate.Kind)
		}
	}
}

func TestNormalizeProfileIDUsesSafeCharacters(t *testing.T) {
	got := normalizeProfileID("  team / demo:账号 ..  ")
	if got != "team-demo" {
		t.Fatalf("normalizeProfileID() = %q", got)
	}
	for _, raw := range []string{"...", "///", "___"} {
		if got := normalizeProfileID(raw); got == "" || got == "." || got == ".." {
			t.Fatalf("normalizeProfileID(%q) = %q", raw, got)
		}
	}
}

func TestProfileReservationRejectsConcurrentUse(t *testing.T) {
	service := New(Config{AgentDockHome: t.TempDir()}, nil)
	profileDir, temporary, err := service.reserveProfile("work")
	if err != nil {
		t.Fatal(err)
	}
	if temporary || filepath.Base(profileDir) != "work" {
		t.Fatalf("profile reservation = %q temporary=%v", profileDir, temporary)
	}

	_, _, err = service.reserveProfile("work")
	var browserErr *Error
	if !errors.As(err, &browserErr) || browserErr.Code != ErrProfileInUse {
		t.Fatalf("second reservation error = %#v, want %s", err, ErrProfileInUse)
	}

	service.releaseProfile("work", profileDir, false)
	if _, _, err := service.reserveProfile("work"); err != nil {
		t.Fatalf("profile was not released: %v", err)
	}
}

func TestCleanupStaleOnlyRemovesOldCurrentProcessSessions(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	service := New(Config{AgentDockHome: t.TempDir()}, nil)
	service.now = func() time.Time { return now }
	oldTemp := filepath.Join(t.TempDir(), "old-temp")
	if err := os.MkdirAll(oldTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	service.sessions["old"] = &session{id: "old", lastActivity: now.Add(-7 * time.Hour), temporaryProfile: true, profileDir: oldTemp, pages: map[target.ID]*pageState{}}
	service.sessions["fresh"] = &session{id: "fresh", lastActivity: now.Add(-time.Hour), profileID: "fresh-profile", pages: map[target.ID]*pageState{}}
	service.profiles["fresh-profile"] = "fresh"

	got := service.cleanupStale(CleanupRequest{})
	if got.RemovedCount != 1 || len(got.RemovedSessions) != 1 || got.RemovedSessions[0] != "old" {
		t.Fatalf("CleanupStale() = %#v", got)
	}
	if _, err := os.Stat(oldTemp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary profile still exists: %v", err)
	}
	if _, err := service.getSession("fresh"); err != nil {
		t.Fatalf("fresh session was removed: %v", err)
	}
}

func TestSessionTableConcurrentAccess(t *testing.T) {
	service := New(Config{AgentDockHome: t.TempDir()}, nil)
	now := time.Now()
	for index := 0; index < 20; index++ {
		id := "session-" + time.Unix(0, int64(index+1)).Format("150405.000000000")
		service.sessions[id] = &session{id: id, createdAt: now, lastActivity: now, pages: make(map[target.ID]*pageState)}
	}

	var wg sync.WaitGroup
	for id := range service.sessions {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				sess, err := service.getSession(id)
				if err == nil {
					sess.touch(now.Add(time.Duration(i) * time.Millisecond))
				}
			}
		}()
	}
	wg.Wait()
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseSessionIsStrictAndServiceCloseIsIdempotent(t *testing.T) {
	service := New(Config{AgentDockHome: t.TempDir()}, nil)
	service.sessions["one"] = &session{id: "one", pages: make(map[target.ID]*pageState)}
	closed, err := service.closeSession(CloseRequest{SessionID: "one"})
	if err != nil || !closed.Closed {
		t.Fatalf("CloseSession() = %#v, %v", closed, err)
	}
	_, err = service.closeSession(CloseRequest{SessionID: "one"})
	var browserErr *Error
	if !errors.As(err, &browserErr) || browserErr.Code != ErrSessionNotFound {
		t.Fatalf("second close error = %#v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestElementStateAndURLMatching(t *testing.T) {
	for _, test := range []struct {
		found, visible bool
		state          ElementState
		want           bool
	}{
		{true, true, StateVisible, true},
		{true, false, StateVisible, false},
		{true, false, StateHidden, true},
		{false, false, StateHidden, true},
		{true, false, StateAttached, true},
		{false, false, StateDetached, true},
	} {
		if got := elementStateMatches(test.found, test.visible, test.state); got != test.want {
			t.Fatalf("elementStateMatches(%v,%v,%q) = %v", test.found, test.visible, test.state, got)
		}
	}
	if !urlMatches("https://example.test/a/42", "https://example.test/a/*") {
		t.Fatal("wildcard URL did not match")
	}
	if urlMatches("https://example.test/a/42", "https://example.test/b/*") {
		t.Fatal("unrelated wildcard URL matched")
	}
}

func TestBrowserErrorClassification(t *testing.T) {
	var browserErr *Error
	if err := classifyLaunchError(context.DeadlineExceeded); !errors.As(err, &browserErr) || browserErr.Code != ErrTimeout {
		t.Fatalf("launch deadline = %#v", err)
	}
	browserErr = nil
	if err := classifyLaunchError(errors.New("boom")); !errors.As(err, &browserErr) || browserErr.Code != ErrLaunchFailed {
		t.Fatalf("launch failure = %#v", err)
	}
	browserErr = nil
	if err := classifyOperationError(context.DeadlineExceeded, "snapshot"); !errors.As(err, &browserErr) || browserErr.Code != ErrTimeout {
		t.Fatalf("operation deadline = %#v", err)
	}
	browserErr = nil
	if err := classifyOperationError(errors.New("boom"), "snapshot"); !errors.As(err, &browserErr) || browserErr.Code != ErrCDPFailed {
		t.Fatalf("CDP failure = %#v", err)
	}
	browserErr = nil
	if err := wrapActionError(errors.New("boom"), 0, "click"); !errors.As(err, &browserErr) || browserErr.Code != ErrActionFailed || browserErr.Details == nil || browserErr.Details.ActionIndex == nil || *browserErr.Details.ActionIndex != 0 {
		t.Fatalf("action failure = %#v", err)
	}
}
