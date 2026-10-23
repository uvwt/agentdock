package acp

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBlockedWriteHonorsRequestTimeout(t *testing.T) {
	incoming, peerWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	peerReader, outgoing, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	connection := NewConnection(incoming, outgoing, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- connection.Request(ctx, "session/prompt", map[string]any{"prompt": strings.Repeat("x", 1<<20)}, nil)
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("write timeout = %v", err)
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("request still blocked in pipe write 350ms after its context deadline")
	}
	_ = peerReader.Close()
	_ = peerWriter.Close()
	_ = connection.Close()
}

type startedPipeWriter struct {
	*os.File
	once    sync.Once
	started chan struct{}
}

func (w *startedPipeWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	return w.File.Write(data)
}

func TestRequestTimeoutWhileAnotherWriteIsBlocked(t *testing.T) {
	incoming, peerWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	peerReader, outgoing, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	writer := &startedPipeWriter{File: outgoing, started: make(chan struct{})}
	connection := NewConnection(incoming, writer, nil, nil)
	defer func() { _ = peerReader.Close(); _ = peerWriter.Close(); _ = connection.Close() }()
	first := make(chan error, 1)
	go func() { first <- connection.Notify("large", map[string]any{"text": strings.Repeat("x", 1<<20)}) }()
	select {
	case <-writer.started:
	case <-time.After(time.Second):
		t.Fatal("first write did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	second := make(chan error, 1)
	go func() { second <- connection.Request(ctx, "queued", nil, nil) }()
	select {
	case err := <-second:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("queued timeout = %v", err)
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("queued write ignored deadline")
	}
	select {
	case <-connection.Closed():
		t.Error("queued cancellation closed unrelated active write")
	default:
	}
	_ = connection.Close()
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Error("active writer did not exit after connection close")
	}
}

func TestAlreadyCancelledRequestDoesNotCloseConnection(t *testing.T) {
	for i := 0; i < 10; i++ {
		reader, writer, cleanup := pipeConnectionPair(t)
		connection := NewConnection(reader, writer, nil, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := connection.Request(ctx, "cancelled", nil, nil)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("request error = %v", err)
		}
		select {
		case <-connection.Closed():
			t.Error("cancelled request invalidated an unused connection")
		default:
		}
		_ = connection.Close()
		cleanup()
	}
}

type failedPipeWriter struct{}

func (failedPipeWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }
func (failedPipeWriter) Close() error              { return nil }

func TestWriteFailureKeepsConnectionErrorClassification(t *testing.T) {
	reader, _, cleanup := pipeConnectionPair(t)
	defer cleanup()
	connection := NewConnection(reader, failedPipeWriter{}, nil, nil)
	defer connection.Close()
	err := connection.Request(context.Background(), "broken", nil, nil)
	var connectionErr *Error
	if !errors.As(err, &connectionErr) || connectionErr.Code != "ACP_CONNECTION_FAILED" {
		t.Fatalf("write failure = %v, want ACP_CONNECTION_FAILED", err)
	}
}
