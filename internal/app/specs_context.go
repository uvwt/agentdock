package app

func contextToolSpecs() []ToolSpec {
	return []ToolSpec{{
		Name: "agentdock_context", Contract: canonicalToolContract, Title: "AgentDock context",
		Description: "Return structured AgentDock bootstrap context including available capabilities, integrations, rules, and high-priority context.",
		Handler:     ctxToolHandler((*Runtime).agentDockContextTool),
	}}
}
