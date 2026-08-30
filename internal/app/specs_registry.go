package app

// buildToolSpecs 只负责公开工具顺序和能力分组。每个能力的描述、handler 与
// contract owner 放在对应 specs_<capability>.go，新增工具不再修改中央 Schema switch。
func buildToolSpecs() []ToolSpec {
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
	return specs
}

var (
	toolSpecs     = buildToolSpecs()
	toolSpecIndex = indexToolSpecs(toolSpecs)
)

func indexToolSpecs(specs []ToolSpec) map[string]ToolSpec {
	index := make(map[string]ToolSpec, len(specs))
	for _, spec := range specs {
		if spec.Name == "" {
			panic("tool spec name must not be empty")
		}
		if spec.Contract == nil {
			panic("tool spec contract must not be nil: " + spec.Name)
		}
		if spec.Handler == nil {
			panic("tool spec handler must not be nil: " + spec.Name)
		}
		if _, exists := index[spec.Name]; exists {
			panic("duplicate tool spec: " + spec.Name)
		}
		index[spec.Name] = spec
	}
	return index
}
