package nexusclient_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/uvwt/agentdock/generated/nexuscontracts"
	"github.com/uvwt/agentdock/internal/commandqueue"
)

func TestExecutorIsDurablyIdempotent(t *testing.T) {
	stateDir := t.TempDir()
	store, err := commandqueue.OpenStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	executor := commandqueue.NewExecutor(store)
	var executions atomic.Int32
	if err := executor.Register(commandqueue.FuncHandler{
		CommandType: "health.check",
		Run: func(context.Context, json.RawMessage, commandqueue.ProgressReporter) (commandqueue.HandlerResult, error) {
			executions.Add(1)
			return commandqueue.HandlerResult{Output: map[string]bool{"ok": true}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lease := testLease(now, "command-1", "lease-1", "same-side-effect", "health.check", time.Minute)
	first, err := executor.Execute(context.Background(), lease, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Result.Status != commandqueue.StatusSucceeded || first.Duplicate {
		t.Fatalf("first execution = %#v", first)
	}

	lease.LeaseId = "lease-2"
	lease.Command.Id = "command-2"
	second, err := executor.Execute(context.Background(), lease, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || second.Result.Status != commandqueue.StatusSucceeded {
		t.Fatalf("duplicate execution = %#v", second)
	}
	if got := executions.Load(); got != 1 {
		t.Fatalf("handler executions = %d, want 1", got)
	}

	reopened, err := commandqueue.OpenStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	restarted := commandqueue.NewExecutor(reopened)
	if err := restarted.Register(commandqueue.FuncHandler{
		CommandType: "health.check",
		Run: func(context.Context, json.RawMessage, commandqueue.ProgressReporter) (commandqueue.HandlerResult, error) {
			executions.Add(1)
			return commandqueue.HandlerResult{}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	third, err := restarted.Execute(context.Background(), lease, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !third.Duplicate || executions.Load() != 1 {
		t.Fatalf("restart duplicate = %#v; executions=%d", third, executions.Load())
	}
}

func TestExecutorRejectsArbitraryCommandAndTimesOutAtLeaseBoundary(t *testing.T) {
	store, err := commandqueue.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	executor := commandqueue.NewExecutor(store)
	now := time.Now().UTC()
	forbidden, err := executor.Execute(context.Background(), testLease(now, "shell", "lease-shell", "shell-side-effect", "shell.exec", time.Minute), nil)
	if err != nil {
		t.Fatal(err)
	}
	if forbidden.Result.Error == nil || forbidden.Result.Error.Code != "COMMAND_NOT_ALLOWED" {
		t.Fatalf("forbidden result = %#v", forbidden.Result)
	}

	if err := executor.Register(commandqueue.FuncHandler{
		CommandType: "diagnostics.collect",
		Run: func(ctx context.Context, _ json.RawMessage, _ commandqueue.ProgressReporter) (commandqueue.HandlerResult, error) {
			<-ctx.Done()
			return commandqueue.HandlerResult{}, ctx.Err()
		},
	}); err != nil {
		t.Fatal(err)
	}
	timeoutLease := testLease(time.Now().UTC(), "timeout", "lease-timeout", "timeout-side-effect", "diagnostics.collect", 250*time.Millisecond)
	timedOut, err := executor.Execute(context.Background(), timeoutLease, nil)
	if err != nil {
		t.Fatal(err)
	}
	if timedOut.Result.Error == nil || timedOut.Result.Error.Code != "LOCAL_TIMEOUT" {
		t.Fatalf("timeout result = %#v", timedOut.Result)
	}
}

func TestExecutorProgressUsesGeneratedContract(t *testing.T) {
	store, err := commandqueue.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	executor := commandqueue.NewExecutor(store)
	var reported atomic.Bool
	if err := executor.Register(commandqueue.FuncHandler{
		CommandType: "health.check",
		Run: func(ctx context.Context, _ json.RawMessage, progress commandqueue.ProgressReporter) (commandqueue.HandlerResult, error) {
			percent := int64(50)
			message := "checking"
			if err := progress.Report(ctx, contracts.CommandProgress{Status: commandqueue.StatusRunning, Percent: &percent, Message: &message}); err != nil {
				return commandqueue.HandlerResult{}, err
			}
			return commandqueue.HandlerResult{Output: map[string]bool{"ok": true}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = executor.Execute(context.Background(), testLease(time.Now().UTC(), "progress", "lease-progress", "progress-side-effect", "health.check", time.Minute), commandqueue.ProgressFunc(func(_ context.Context, progress contracts.CommandProgress) error {
		if progress.Percent != nil && *progress.Percent == 50 {
			reported.Store(true)
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !reported.Load() {
		t.Fatal("progress was not reported")
	}
}
