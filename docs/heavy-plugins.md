# Heavy plugins and capability switches

AgentDock heavy plugins group installed document Skills and registered dynamic MCP servers behind one domain-level capability description. The grouping is logical: a plugin does not copy, repackage, or replace its members, so existing Skill packages and MCP registrations remain compatible.

## Progressive disclosure

An AgentDock client discovers a heavy plugin in two stages:

1. `agentdock_context` exposes the enabled plugin's name, short description, Skill count, and MCP server count. Plugin-owned members are omitted from the top-level `skills` and `dynamic_mcp` indexes.
2. `plugin_load` expands one enabled plugin and returns the descriptions and entry points of its currently enabled members. It lazily connects to each plugin-owned MCP server at this stage and includes every discovered MCP tool's name, qualified name, title, and description. The client can then read a returned `skill://.../SKILL.md` resource or inspect and call a returned MCP tool.

A generic `mcp_tool_search` request does not search plugin-owned MCP servers. After `plugin_load`, use the returned tool descriptions directly, or pass the returned server name to `mcp_tool_search` to refresh or narrow the tool index before inspecting and calling a tool.

## Availability model

Each Skill, MCP server, and plugin has its own persisted switch. Effective availability is:

```text
member base enabled AND (member has no plugin OR owner plugin enabled)
```

Disabling a plugin overlays all of its members without changing their individual switches. Re-enabling the plugin restores every member whose own switch is still enabled. Disabling an individual member keeps that member unavailable after the plugin is re-enabled.

The runtime enforces effective availability at the operation boundary:

- disabled Skills cannot be resolved through `skill://` resources or active Skill context;
- disabled or hidden MCP servers cannot be searched, inspected, or called;
- disabled plugins cannot be expanded with `plugin_load`;
- a member can belong to only one plugin of the same member type.

## Persistence and compatibility

Plugin definitions are stored in:

```text
~/.agentdock/plugins/plugins.json
```

The registry contains only plugin ownership, description, and plugin-level state. Skill versions, activation history, Skill-level state, MCP transports, credentials, and MCP-level state remain in their existing stores.

A missing plugin registry is treated as an empty registry. Existing installations therefore preserve their previous flat Skill and MCP behavior until a plugin is created.

## Management

The Windows control panel includes a **Capabilities** page that:

- creates, edits, enables, disables, and removes plugins;
- enables or disables individual Skills and MCP servers;
- renders plugin members inside their plugin card;
- excludes plugin-owned members from the standalone Skill and MCP lists.

Removing a plugin removes only its grouping definition. Its Skills and MCP servers remain installed and return to the standalone lists.

The same operations are available through `plugin_manage`, `skill_package`, `mcp_manage`, and the authenticated local Runtime API.
