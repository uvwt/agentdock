package app

import (
	protocol "github.com/uvwt/agentdock-protocol"
	"github.com/uvwt/agentdock/internal/config"
)

// UITrigger describes whether a binding is descriptor-level or scoped to one tool action.
type UITrigger struct {
	Action string
}

// UIBinding describes when a tool should attach an MCP App resource to its descriptor or result.
// Full preserves today's behavior. Compact is opt-in per binding so new high-frequency views
// do not silently enter compact mode.
type UIBinding struct {
	ResourceURI string
	Action      string
	Compact     *UITrigger
}

func (binding UIBinding) Trigger(mode config.MCPAppsMode) (UITrigger, bool) {
	switch mode {
	case config.MCPAppsModeOff:
		return UITrigger{}, false
	case config.MCPAppsModeCompact:
		if binding.Compact == nil {
			return UITrigger{}, false
		}
		return *binding.Compact, true
	default:
		return UITrigger{Action: binding.Action}, true
	}
}

var toolUIBindings = map[string]UIBinding{
	"view_image":        {ResourceURI: protocol.ImageUIResourceURI, Compact: &UITrigger{}},
	"agentdock_context": {ResourceURI: protocol.ContextUIResourceURI, Compact: &UITrigger{}},
	"workspace_context": {ResourceURI: protocol.WorkspaceUIResourceURI, Compact: &UITrigger{}},
	"file_edit":         {ResourceURI: protocol.FileChangeUIResourceURI},
	"task_manage":       {ResourceURI: protocol.TaskProgressUIResourceURI},
	"acp_session":       {ResourceURI: protocol.ACPStatusUIResourceURI},
	"workflow_template_manage": {
		ResourceURI: protocol.WorkflowUIResourceURI,
		Action:      "match",
	},
	"mcp_tool_call": {ResourceURI: protocol.DynamicMCPUIResourceURI},
	"recall_write":  {ResourceURI: protocol.RecallUIResourceURI},
	"file_publish": {
		ResourceURI: protocol.ArtifactUIResourceURI,
		Compact:     &UITrigger{},
	},
}

func toolUIBinding(name string) *UIBinding {
	binding, ok := toolUIBindings[name]
	if !ok {
		return nil
	}
	cloned := binding
	return &cloned
}
