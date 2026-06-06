package nexusclient_test

import (
	"context"
	"errors"
	"testing"

	"github.com/uvwt/agentdock/internal/commandqueue"
)

func TestOutboxPersistsDeduplicatesAndRecovers(t *testing.T) {
	stateDir := t.TempDir()
	outbox, err := commandqueue.OpenOutbox(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := outbox.Put(commandqueue.OutboxCommandResult, "command-1", map[string]string{"status": "failed"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := outbox.Put(commandqueue.OutboxCommandResult, "command-1", map[string]string{"status": "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("stable outbox id mismatch: %q != %q", first.ID, second.ID)
	}

	reopened, err := commandqueue.OpenOutbox(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := reopened.Pending(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != first.ID {
		t.Fatalf("pending = %#v", pending)
	}

	offline := errors.New("nexus offline")
	uploaded, err := reopened.Drain(context.Background(), 10, func(context.Context, commandqueue.Envelope) error {
		return offline
	})
	if !errors.Is(err, offline) || uploaded != 0 {
		t.Fatalf("offline drain uploaded=%d err=%v", uploaded, err)
	}
	pending, err = reopened.Pending(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Attempts != 1 || pending[0].LastError == "" {
		t.Fatalf("attempt metadata = %#v", pending)
	}

	uploaded, err = reopened.Drain(context.Background(), 10, func(context.Context, commandqueue.Envelope) error {
		return nil
	})
	if err != nil || uploaded != 1 {
		t.Fatalf("recovery drain uploaded=%d err=%v", uploaded, err)
	}
	pending, err = reopened.Pending(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("outbox not empty: %#v", pending)
	}
}
