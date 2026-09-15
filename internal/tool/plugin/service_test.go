package plugin

import (
	"context"
	"errors"
	"testing"

	registry "github.com/uvwt/agentdock/internal/plugin"
	toolcore "github.com/uvwt/agentdock/internal/tool/core"
)

func TestManageAndLoadPluginMembers(t *testing.T) {
	store, err := registry.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := New(
		store,
		func(name string) (SkillItem, bool, error) {
			if name != "layout" {
				return SkillItem{}, false, nil
			}
			return SkillItem{Name: name, Description: "Layout workflow", File: "skill://layout/SKILL.md", Enabled: true}, true, nil
		},
		func(_ context.Context, name string, expand bool) (MCPItem, bool, error) {
			if name != "easyeda" {
				return MCPItem{}, false, nil
			}
			item := MCPItem{Name: name, Description: "EasyEDA tools", Status: "ready", ToolCount: 1, Enabled: true}
			if expand {
				item.Tools = []MCPToolItem{{Name: "route", QualifiedName: "easyeda:route", Description: "Route PCB traces", Server: "easyeda"}}
			}
			return item, true, nil
		},
	)

	result, err := service.Manage(context.Background(), ManageRequest{
		Action: "upsert", Name: "pcb", Description: "PCB design capabilities.",
		Skills: []string{"layout"}, MCPServers: []string{"easyeda"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := result["plugin"].(registry.Definition)
	if !ok || stored.Name != "pcb" || !stored.Enabled {
		t.Fatalf("stored plugin = %#v", result["plugin"])
	}

	loaded, err := service.Load(context.Background(), LoadRequest{Name: "pcb"})
	if err != nil {
		t.Fatal(err)
	}
	skills, ok := loaded["skills"].([]SkillItem)
	if !ok || len(skills) != 1 || skills[0].File != "skill://layout/SKILL.md" {
		t.Fatalf("loaded skills = %#v", loaded["skills"])
	}
	servers, ok := loaded["mcp_servers"].([]MCPItem)
	if !ok || len(servers) != 1 || servers[0].Name != "easyeda" || servers[0].ToolCount != 1 || len(servers[0].Tools) != 1 || servers[0].Tools[0].QualifiedName != "easyeda:route" {
		t.Fatalf("loaded MCP servers = %#v", loaded["mcp_servers"])
	}
	unavailable, ok := loaded["unavailable_members"].([]map[string]any)
	if !ok || len(unavailable) != 0 {
		t.Fatalf("unavailable members = %#v", loaded["unavailable_members"])
	}
}

func TestLoadReportsDisabledBaseMembers(t *testing.T) {
	store, err := registry.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := New(
		store,
		func(name string) (SkillItem, bool, error) {
			return SkillItem{Name: name, Enabled: false}, true, nil
		},
		func(_ context.Context, name string, _ bool) (MCPItem, bool, error) {
			return MCPItem{Name: name, Enabled: false}, true, nil
		},
	)
	if _, err := service.Manage(context.Background(), ManageRequest{
		Action: "upsert", Name: "domain", Description: "Domain capabilities.",
		Skills: []string{"disabled-skill"}, MCPServers: []string{"disabled-mcp"},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.Load(context.Background(), LoadRequest{Name: "domain"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(loaded["skills"].([]SkillItem)); got != 0 {
		t.Fatalf("enabled skills = %d, want 0", got)
	}
	if got := len(loaded["mcp_servers"].([]MCPItem)); got != 0 {
		t.Fatalf("enabled MCP servers = %d, want 0", got)
	}
	unavailable := loaded["unavailable_members"].([]map[string]any)
	if len(unavailable) != 2 || unavailable[0]["reason"] != "disabled" || unavailable[1]["reason"] != "disabled" {
		t.Fatalf("unavailable = %#v", unavailable)
	}
}

func TestLoadKeepsSkillsWhenMCPToolDiscoveryFails(t *testing.T) {
	store, err := registry.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := New(
		store,
		func(name string) (SkillItem, bool, error) {
			return SkillItem{Name: name, File: "skill://workflow/SKILL.md", Enabled: true}, true, nil
		},
		func(_ context.Context, name string, _ bool) (MCPItem, bool, error) {
			return MCPItem{
				Name: name, Enabled: true, Status: "error", LastErrorCode: "MCP_TIMEOUT",
				ToolLoadError: "tools/list timed out",
			}, true, nil
		},
	)
	if _, err := service.Manage(context.Background(), ManageRequest{
		Action: "upsert", Name: "domain", Description: "Domain capabilities.",
		Skills: []string{"workflow"}, MCPServers: []string{"remote"},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.Load(context.Background(), LoadRequest{Name: "domain"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(loaded["skills"].([]SkillItem)); got != 1 {
		t.Fatalf("loaded Skills = %d, want 1", got)
	}
	servers := loaded["mcp_servers"].([]MCPItem)
	if len(servers) != 1 || servers[0].ToolLoadError == "" {
		t.Fatalf("loaded MCP servers = %#v", servers)
	}
	unavailable := loaded["unavailable_members"].([]map[string]any)
	if len(unavailable) != 1 || unavailable[0]["reason"] != "tool_discovery_failed" || unavailable[0]["code"] != "MCP_TIMEOUT" {
		t.Fatalf("unavailable members = %#v", unavailable)
	}
}

func TestDisabledPluginCannotLoad(t *testing.T) {
	store, err := registry.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := New(
		store,
		func(name string) (SkillItem, bool, error) { return SkillItem{Name: name, Enabled: true}, true, nil },
		func(context.Context, string, bool) (MCPItem, bool, error) { return MCPItem{}, false, nil },
	)
	if _, err := service.Manage(context.Background(), ManageRequest{
		Action: "upsert", Name: "domain", Description: "Domain capabilities.", Skills: []string{"skill"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Manage(context.Background(), ManageRequest{Action: "disable", Name: "domain"}); err != nil {
		t.Fatal(err)
	}
	_, err = service.Load(context.Background(), LoadRequest{Name: "domain"})
	var toolErr *toolcore.ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "PLUGIN_DISABLED" {
		t.Fatalf("Load disabled plugin error = %#v", err)
	}
}

func TestUpsertRejectsUnknownMember(t *testing.T) {
	store, err := registry.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := New(
		store,
		func(string) (SkillItem, bool, error) { return SkillItem{}, false, nil },
		func(context.Context, string, bool) (MCPItem, bool, error) { return MCPItem{}, false, nil },
	)
	_, err = service.Manage(context.Background(), ManageRequest{
		Action: "upsert", Name: "domain", Description: "Domain capabilities.", Skills: []string{"missing"},
	})
	var toolErr *toolcore.ToolError
	if !errors.As(err, &toolErr) || toolErr.Code != "PLUGIN_MEMBER_NOT_FOUND" {
		t.Fatalf("unknown member error = %#v", err)
	}
}
