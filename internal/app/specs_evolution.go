package app

func evolutionToolSpecs() []ToolSpec {
	return []ToolSpec{{
		Name: "evolve", Title: "Evolve AgentDock knowledge",
		Description: "Propose reusable knowledge, pre-bind Task learning checks, supersede, or retract. Bind must happen before execution and declares on_success/on_failure semantics; AgentDock resolves later Task outcomes and owns lifecycle policy while Recall only persists the result.",
		Annotations: mutatingToolAnnotations(true, false), Availability: requiresNexus, Handler: ctxToolHandler((*Runtime).evolve),
	}}
}
