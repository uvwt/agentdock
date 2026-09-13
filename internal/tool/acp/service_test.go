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

	defaultResult, err := service.Interaction(context.Background(), InteractionRequest{Action: "list"})
	if err != nil {
		t.Fatal(err)
	}
	if defaultResult["profile_id"] != "zcode" {
		t.Fatalf("default interaction profile_id = %#v", defaultResult["profile_id"])
	}
	explicitResult, err := service.Interaction(context.Background(), InteractionRequest{ProfileID: "agy", Action: "list"})
	if err != nil {
		t.Fatal(err)
	}
	if explicitResult["profile_id"] != "agy" {
		t.Fatalf("explicit interaction profile_id = %#v", explicitResult["profile_id"])
	}

	for _, request := range []SessionRequest{
		{Action: "update", SessionID: "acps_missing"},
		{Action: "update", SessionID: "acps_missing", ModeID: "code", ConfigID: "safe", ConfigValue: true},
	} {
		_, callErr := service.Session(context.Background(), request)
		var toolErr *ToolError
		if !errors.As(callErr, &toolErr) || toolErr.Code != "ACP_SESSION_UPDATE_INVALID" {
			t.Fatalf("invalid update error = %#v", callErr)
		}
	}

	_, callErr := service.Session(context.Background(), SessionRequest{Action: "future_action", AuthMethodID: "would-start-adapter"})
	var invalidActionErr *ToolError
	if !errors.As(callErr, &invalidActionErr) || invalidActionErr.Code != "ACP_ACTION_INVALID" {
		t.Fatalf("invalid action error = %#v", callErr)
	}

	for _, request := range []InteractionRequest{
		{Action: "respond", InteractionID: "acpi_missing"},
		{Action: "respond", InteractionID: "acpi_missing", Response: InteractionResponseInput{Action: "cancel", OptionID: "allow_once"}},
		{Action: "respond", InteractionID: "acpi_missing", Response: InteractionResponseInput{Action: "accept"}},
	} {
		_, callErr := service.Interaction(context.Background(), request)
		var toolErr *ToolError
		if !errors.As(callErr, &toolErr) || toolErr.Code != "ACP_INTERACTION_RESPONSE_INVALID" {
			t.Fatalf("invalid interaction response error = %#v", callErr)
		}
	}

	_, _, err = service.managerFor("missing")
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "ACP_PROFILE_NOT_FOUND" {
		t.Fatalf("missing profile error = %#v", err)
	}
}
