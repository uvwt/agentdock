package session

import (
	"io"
	"strings"
	"testing"
)

func TestInitialInputPrecedesFollowingWrites(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	s := &Session{Stdin: writer, Cancel: func() {}}
	initial := s.StartInput("first-", false)
	following := make(chan error, 1)
	go func() {
		err := s.Write("second")
		_ = s.CloseStdin()
		following <- err
	}()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-initial; err != nil {
		t.Fatal(err)
	}
	if err := <-following; err != nil {
		t.Fatal(err)
	}
	if string(data) != "first-second" {
		t.Fatalf("input = %q, want first-second", data)
	}
}

func TestInitialInputFailureRemainsObservable(t *testing.T) {
	reader, writer := io.Pipe()
	_ = reader.Close()
	defer writer.Close()
	s := &Session{Stdin: writer, Cancel: func() {}, exitCode: 0}
	if err := <-s.StartInput("input", true); err == nil {
		t.Fatal("closed input pipe should fail")
	}
	if err := s.WaitError(); err == nil || !strings.Contains(err.Error(), "write command stdin") {
		t.Fatalf("WaitError = %v, want initial input error", err)
	}
	if s.Snapshot("exited", 1024).CommandOK {
		t.Fatal("input failure should not be reported as successful")
	}
}
