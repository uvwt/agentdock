package acp

import (
	"context"
	"errors"
	"os"
	"testing"

	acpruntime "github.com/uvwt/agentdock/internal/acp"
)

func TestMultiServiceRoutesDefaultAndExplicitProfiles(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	cwd := t.TempDir()
	zcode, err := acpruntime.NewManager(acpruntime.Options{
		Home: home, DefaultCWD: cwd,
		Agent: acpruntime.AgentSpec{Name: "zcode", Command: executable},
	})
	if err != nil {
		t.Fatal(err)
	}
	agy, err := acpruntime.NewManager(acpruntime.Options{
		Home: home, DefaultCWD: cwd,
		Agent: acpruntime.AgentSpec{Name: "agy", Command: executable},
	})
	if err != nil {
		_ = zcode.Close()
		t.Fatal(err)
	}

	service := NewMulti("zcode", map[string]*acpruntime.Manager{"zcode": zcode, "agy": agy})
	defer func() { _ = service.Close() }()

	manager, profileID, err := service.managerFor("")
	if err != nil {
		t.Fatal(err)
	}
	if manager != zcode || profileID != "zcode" {
		t.Fatalf("default route = manager %p profile %q", manager, profileID)
	}

	manager, profileID, err = service.managerFor("agy")
	if err != nil {
		t.Fatal(err)
	}
	if manager != agy || profileID != "agy" {
		t.Fatalf("explicit route = manager %p profile %q", manager, profileID)
	}

	defaultResult, err := service.Session(context.Background(), SessionRequest{Action: "list"})
	if err != nil {
		t.Fatal(err)
	}
	if defaultResult["profile_id"] != "zcode" {
		t.Fatalf("default response profile_id = %#v", defaultResult["profile_id"])
	}
	explicitResult, err := service.Session(context.Background(), SessionRequest{ProfileID: "agy", Action: "list"})
	if err != nil {
		t.Fatal(err)
	}
	if explicitResult["profile_id"] != "agy" {
		t.Fatalf("explicit response profile_id = %#v", explicitResult["profile_id"])
	}

	_, _, err = service.managerFor("missing")
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "ACP_PROFILE_NOT_FOUND" {
		t.Fatalf("missing profile error = %#v", err)
	}
}
