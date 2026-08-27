package app

import protocol "github.com/uvwt/agentdock-protocol"

// UIBinding describes when a tool should attach an MCP App resource to its descriptor or result.
// It is deliberately separate from the resource registry: binding a result does not prove that a node can serve the resource.
type UIBinding struct {
	ResourceURI string
	Action      string
}

var toolUIBindings = map[string]UIBinding{
	"agentdock_context":        {ResourceURI: protocol.ContextUIResourceURI},
	"file_edit":                {ResourceURI: protocol.FileChangeUIResourceURI},
	"task_manage":              {ResourceURI: protocol.TaskProgressUIResourceURI},
	"acp_session":              {ResourceURI: protocol.ACPStatusUIResourceURI},
	"workflow_template_manage": {ResourceURI: protocol.WorkflowUIResourceURI, Action: "match"},
	"mcp_tool_call":            {ResourceURI: protocol.DynamicMCPUIResourceURI},
	"recall_write":             {ResourceURI: protocol.RecallUIResourceURI},
	"file_publish":             {ResourceURI: protocol.ArtifactUIResourceURI},
}

func toolUIBinding(name string) *UIBinding {
	binding, ok := toolUIBindings[name]
	if !ok {
		return nil
	}
	cloned := binding
	return &cloned
}
