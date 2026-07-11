package session

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestKillSkipsCompletedSession(t *testing.T) {
	canceled := false
	s := &Session{
		Command:   &exec.Cmd{Process: &os.Process{Pid: 1 << 30}},
		Cancel:    func() { canceled = true },
		completed: true,
	}
	if killed := s.Kill(); killed {
		t.Fatal("Kill() reported a signal for a completed session")
	}
	if canceled {
		t.Fatal("Kill() canceled an already completed session")
	}
}

func TestStartCapturesCompleteOutputAndExitState(t *testing.T) {
	requirePOSIXShell(t)
	s, _, err := Start(
		context.Background(),
		"printf 'stdout-value'; printf 'stderr-value' >&2; exit 7",
		t.TempDir(),
		os.Environ(),
		2*time.Second,
		nil,
	)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Cancel()
	<-s.Done

	if err := s.WaitError(); err == nil {
		t.Fatal("WaitError() = nil, want exit error")
	}
	result := s.Snapshot("exited", 1<<20)
	if result["stdout"] != "stdout-value" {
		t.Fatalf("stdout = %#v", result["stdout"])
	}
	if result["stderr"] != "stderr-value" {
		t.Fatalf("stderr = %#v", result["stderr"])
	}
	if result["exit_code"] != 7 {
		t.Fatalf("exit_code = %#v, want 7", result["exit_code"])
	}
	if result["stdout_total_bytes"] != len("stdout-value") || result["stderr_total_bytes"] != len("stderr-value") {
		t.Fatalf("byte totals = stdout:%#v stderr:%#v", result["stdout_total_bytes"], result["stderr_total_bytes"])
	}
}

func TestStartCapturesFastCommandOutputRepeatedly(t *testing.T) {
	requirePOSIXShell(t)
	const iterations = 200
	for iteration := range iterations {
		s, _, err := Start(
			context.Background(),
			"printf 'fast-output'",
			t.TempDir(),
			os.Environ(),
			time.Second,
			nil,
		)
		if err != nil {
			t.Fatalf("iteration %d Start() error = %v", iteration, err)
		}
		<-s.Done
		s.Cancel()
		if err := s.WaitError(); err != nil {
			t.Fatalf("iteration %d WaitError() = %v", iteration, err)
		}
		result := s.Snapshot("exited", 1024)
		if result["stdout"] != "fast-output" {
			t.Fatalf("iteration %d stdout = %#v, want fast-output", iteration, result["stdout"])
		}
		if result["stdout_total_bytes"] != len("fast-output") {
			t.Fatalf("iteration %d stdout_total_bytes = %#v", iteration, result["stdout_total_bytes"])
		}
	}
}

func TestStartCompletionSignalSupportsMultipleObservers(t *testing.T) {
	requirePOSIXShell(t)
	s, _, err := Start(context.Background(), "exit 0", t.TempDir(), os.Environ(), time.Second, nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Cancel()
	<-s.Done

	for observer := 0; observer < 3; observer++ {
		select {
		case <-s.Done:
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("observer %d blocked on completed session", observer)
		}
	}
	if err := s.WaitError(); err != nil {
		t.Fatalf("WaitError() = %v, want nil", err)
	}
}

func TestStartWaitsForOutputReadersBeforeCompletion(t *testing.T) {
	requirePOSIXShell(t)
	const lineCount = 4000
	command := "i=0; while [ $i -lt 4000 ]; do printf 'line-%04d\\n' \"$i\"; i=$((i+1)); done"
	s, _, err := Start(context.Background(), command, t.TempDir(), os.Environ(), 5*time.Second, nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Cancel()
	<-s.Done
	if err := s.WaitError(); err != nil {
		t.Fatalf("WaitError() = %v", err)
	}

	result := s.Snapshot("exited", 1<<20)
	stdout, ok := result["stdout"].(string)
	if !ok {
		t.Fatalf("stdout type = %T", result["stdout"])
	}
	if got := strings.Count(stdout, "\n"); got != lineCount {
		t.Fatalf("stdout line count = %d, want %d", got, lineCount)
	}
	if !strings.HasSuffix(stdout, "line-3999\n") {
		t.Fatalf("stdout tail is incomplete: %q", tail(stdout, 40))
	}
}

func TestStartMarksTimeout(t *testing.T) {
	requirePOSIXShell(t)
	s, _, err := Start(context.Background(), "sleep 1", t.TempDir(), os.Environ(), 30*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer s.Cancel()
	<-s.Done

	if !s.TimedOut {
		t.Fatal("TimedOut = false, want true")
	}
	if err := s.WaitError(); err == nil {
		t.Fatal("WaitError() = nil, want timeout termination error")
	}
	result := s.Snapshot("timeout", 1024)
	if result["timed_out"] != true {
		t.Fatalf("snapshot timed_out = %#v", result["timed_out"])
	}
	if _, ok := result["exit_code"]; !ok {
		t.Fatalf("snapshot missing exit_code: %#v", result)
	}
}

func TestStartRejectsNonPositiveTimeout(t *testing.T) {
	_, _, err := Start(context.Background(), "true", t.TempDir(), os.Environ(), 0, nil)
	if err == nil || !strings.Contains(err.Error(), "timeout must be positive") {
		t.Fatalf("Start() error = %v, want timeout validation", err)
	}
}

func TestSnapshotReturnsOnlyNewOutput(t *testing.T) {
	s := &Session{ID: "test", StartedAt: time.Now(), exitCode: -1}
	_, _ = s.stdout.WriteString("first\n")
	first := s.Snapshot("running", 1024)
	if first["stdout"] != "first\n" {
		t.Fatalf("first stdout = %#v", first["stdout"])
	}
	second := s.Snapshot("running", 1024)
	if second["stdout"] != "" {
		t.Fatalf("second stdout = %#v, want empty delta", second["stdout"])
	}
	_, _ = s.stdout.WriteString("second\n")
	third := s.Snapshot("running", 1024)
	if third["stdout"] != "second\n" {
		t.Fatalf("third stdout = %#v", third["stdout"])
	}
}

func TestNewIDIsUniqueUnderConcurrency(t *testing.T) {
	const count = 500
	ids := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	wg.Add(count)
	for range count {
		go func() {
			defer wg.Done()
			id, err := newID()
			if err != nil {
				errs <- err
				return
			}
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	if err := <-errs; err != nil {
		t.Fatalf("newID() error = %v", err)
	}
	seen := make(map[string]struct{}, count)
	for id := range ids {
		if !strings.HasPrefix(id, "session-") {
			t.Fatalf("id = %q, want session- prefix", id)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate id = %q", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != count {
		t.Fatalf("unique ids = %d, want %d", len(seen), count)
	}
}

func TestStoreListIsDeterministic(t *testing.T) {
	store := NewStore()
	started := time.Now()
	store.Add(&Session{ID: "session-b", StartedAt: started})
	store.Add(&Session{ID: "session-c", StartedAt: started.Add(time.Second)})
	store.Add(&Session{ID: "session-a", StartedAt: started})
	listed := store.List()
	if len(listed) != 3 || listed[0].ID != "session-a" || listed[1].ID != "session-b" || listed[2].ID != "session-c" {
		t.Fatalf("session order = %#v", []string{listed[0].ID, listed[1].ID, listed[2].ID})
	}
}

func TestStorePrunesOnlyExpiredCompletedSessions(t *testing.T) {
	store := NewStore()
	now := time.Now()
	store.Add(&Session{ID: "old", StartedAt: now.Add(-2 * time.Hour), FinishedAt: now.Add(-90 * time.Minute), completed: true})
	store.Add(&Session{ID: "recent", StartedAt: now.Add(-time.Minute), FinishedAt: now.Add(-time.Second), completed: true})
	store.Add(&Session{ID: "running", StartedAt: now.Add(-2 * time.Hour)})
	if removed := store.PruneCompletedBefore(now.Add(-time.Hour)); removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := store.Get("old"); ok {
		t.Fatal("expired completed session remained")
	}
	if _, ok := store.Get("recent"); !ok {
		t.Fatal("recent completed session was removed")
	}
	if _, ok := store.Get("running"); !ok {
		t.Fatal("running session was removed")
	}
}

func TestStoreConcurrentAccess(t *testing.T) {
	store := NewStore()
	const count = 100
	var wg sync.WaitGroup
	wg.Add(count)
	for i := range count {
		go func() {
			defer wg.Done()
			id := "session-" + strconv.Itoa(i)
			store.Add(&Session{ID: id})
			_, _ = store.Get(id)
			store.Delete(id)
		}()
	}
	wg.Wait()
	if got := len(store.List()); got != 0 {
		t.Fatalf("store session count = %d, want 0", got)
	}
}

func requirePOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test command uses POSIX shell syntax")
	}
}

func tail(value string, size int) string {
	if len(value) <= size {
		return value
	}
	return value[len(value)-size:]
}

func TestPeekDoesNotConsumeOutput(t *testing.T) {
	s := &Session{ID: "test", StartedAt: time.Now(), exitCode: -1}
	_, _ = s.stdout.WriteString("pending\n")
	peeked := s.Peek("running", 1024)
	if peeked["stdout"] != "pending\n" {
		t.Fatalf("peek stdout = %#v", peeked["stdout"])
	}
	observed := s.Snapshot("running", 1024)
	if observed["stdout"] != "pending\n" {
		t.Fatalf("snapshot after peek stdout = %#v", observed["stdout"])
	}
}
