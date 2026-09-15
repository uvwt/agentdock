package plugin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreTreatsMissingRegistryAsEmpty(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plugins, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 0 {
		t.Fatalf("plugins = %#v, want empty", plugins)
	}
}

func TestStorePersistsDefinitionsAndMembership(t *testing.T) {
	home := t.TempDir()
	store, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.Upsert(Definition{
		Name: "pcb", Description: "PCB design capabilities.", Enabled: true,
		Skills: []string{"routing", "layout", "layout"}, MCPServers: []string{"easyeda"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Skills) != 2 || created.Skills[0] != "layout" || created.Skills[1] != "routing" {
		t.Fatalf("normalized skills = %#v", created.Skills)
	}

	reopened, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	membership, ok, err := reopened.SkillMembership("layout")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || membership.Plugin != "pcb" || !membership.Enabled {
		t.Fatalf("skill membership = %#v ok=%v", membership, ok)
	}
	membership, ok, err = reopened.MCPMembership("easyeda")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || membership.Plugin != "pcb" || !membership.Enabled {
		t.Fatalf("MCP membership = %#v ok=%v", membership, ok)
	}

	disabled, err := reopened.SetEnabled("pcb", false)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled {
		t.Fatalf("disabled plugin = %#v", disabled)
	}
	membership, ok, err = reopened.MCPMembership("easyeda")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || membership.Enabled {
		t.Fatalf("disabled membership = %#v ok=%v", membership, ok)
	}
}

func TestStoreRejectsDuplicateMemberOwnership(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(Definition{Name: "one", Description: "One.", Enabled: true, Skills: []string{"shared"}}); err != nil {
		t.Fatal(err)
	}
	_, err = store.Upsert(Definition{Name: "two", Description: "Two.", Enabled: true, Skills: []string{"shared"}})
	var pluginErr *Error
	if !errors.As(err, &pluginErr) || pluginErr.Code != "PLUGIN_MEMBER_CONFLICT" {
		t.Fatalf("conflict error = %#v", err)
	}
}

func TestStoreUpsertCanMoveMembersWithinSamePlugin(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(Definition{Name: "pcb", Description: "PCB.", Enabled: true, Skills: []string{"layout"}}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Upsert(Definition{Name: "pcb", Description: "PCB updated.", Enabled: false, MCPServers: []string{"easyeda"}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Description != "PCB updated." || updated.Enabled || len(updated.Skills) != 0 || len(updated.MCPServers) != 1 {
		t.Fatalf("updated plugin = %#v", updated)
	}
	if _, ok, err := store.SkillMembership("layout"); err != nil || ok {
		t.Fatalf("stale skill membership ok=%v err=%v", ok, err)
	}
}

func TestStoreRejectsUnknownRegistryFields(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "plugins", "plugins.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"version": 1,
		"plugins": []map[string]any{{
			"name": "pcb", "description": "PCB.", "enabled": true, "skills": []string{"layout"}, "unknown": true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = New(home)
	var pluginErr *Error
	if !errors.As(err, &pluginErr) || pluginErr.Code != "PLUGIN_STORE_INVALID" {
		t.Fatalf("invalid registry error = %#v", err)
	}
}
