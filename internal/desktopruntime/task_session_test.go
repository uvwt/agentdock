package desktopruntime

import (
	"strings"
	"testing"
)

func TestSelectInteractiveTaskSessionIDPrefersCallerSession(t *testing.T) {
	target := "S-1-5-21-1000-1000-1000-1001"
	sessions := []InteractiveSession{
		{SessionID: 2, State: 0, UserSID: target},
		{SessionID: 4, State: 0, UserSID: target},
	}
	got, err := SelectInteractiveTaskSessionID(sessions, target, 2, target, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("session=%d, want 2", got)
	}
}

func TestSelectInteractiveTaskSessionIDFromSessionZeroUsesUniqueActive(t *testing.T) {
	target := "S-1-5-21-1000-1000-1000-1001"
	sessions := []InteractiveSession{
		{SessionID: 2, State: 0, UserSID: target},
		{SessionID: 3, State: 4, UserSID: target},
	}
	got, err := SelectInteractiveTaskSessionID(sessions, target, 0, "S-1-5-18", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Fatalf("session=%d, want 2", got)
	}
}

func TestSelectInteractiveTaskSessionIDUsesConsoleWhenAmbiguous(t *testing.T) {
	target := "S-1-5-21-1000-1000-1000-1001"
	sessions := []InteractiveSession{
		{SessionID: 2, State: 0, UserSID: target},
		{SessionID: 4, State: 0, UserSID: target},
	}
	got, err := SelectInteractiveTaskSessionID(sessions, target, 0, "S-1-5-18", 4)
	if err != nil {
		t.Fatal(err)
	}
	if got != 4 {
		t.Fatalf("session=%d, want 4", got)
	}
}

func TestSelectInteractiveTaskSessionIDRefusesAmbiguousSessions(t *testing.T) {
	target := "S-1-5-21-1000-1000-1000-1001"
	sessions := []InteractiveSession{
		{SessionID: 2, State: 0, UserSID: target},
		{SessionID: 4, State: 0, UserSID: target},
	}
	_, err := SelectInteractiveTaskSessionID(sessions, target, 0, "S-1-5-18", 9)
	if err == nil || !strings.Contains(err.Error(), "Multiple active interactive Windows sessions") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSelectInteractiveTaskSessionIDRequiresActiveSession(t *testing.T) {
	target := "S-1-5-21-1000-1000-1000-1001"
	sessions := []InteractiveSession{
		{SessionID: 2, State: 4, UserSID: target},
		{SessionID: 0, State: 0, UserSID: "S-1-5-18"},
	}
	_, err := SelectInteractiveTaskSessionID(sessions, target, 0, "S-1-5-18", noConsoleSessionID)
	if err == nil || !strings.Contains(err.Error(), "No active interactive Windows session") {
		t.Fatalf("unexpected error: %v", err)
	}
}
