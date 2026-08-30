package app

// allToolSpecs 只负责公开工具顺序和能力分组。每个能力的描述与 handler
// 放在对应 specs_<capability>.go，避免所有工具修改都碰同一个大字面量。
func allToolSpecs() []ToolSpec {
	specs := make([]ToolSpec, 0, 32)
	specs = append(specs, contextToolSpecs()...)
	specs = append(specs, fileToolSpecs()...)
	specs = append(specs, commandToolSpecs()...)
	specs = append(specs, taskManageToolSpecs()...)
	specs = append(specs, evolutionToolSpecs()...)
	specs = append(specs, acpToolSpecs()...)
	specs = append(specs, workflowToolSpecs()...)
	specs = append(specs, skillToolSpecs()...)
	specs = append(specs, dynamicMCPToolSpecs()...)
	specs = append(specs, imageToolSpecs()...)
	specs = append(specs, recallToolSpecs()...)
	specs = append(specs, browserToolSpecs()...)
	specs = append(specs, publishToolSpecs()...)
	return bindToolSchemas(specs)
}
