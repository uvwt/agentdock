package mcp

const (
	baseServerInstructions  = "优先调用 `agentdock_context` 获取可用于操作用户设备的核心能力、Skill、动态 MCP 和重要上下文。操作具体项目、切换工作区或工作区规则可能变化时，先调用 `workspace_context` 获取当前工作区上下文。处理多步骤任务时先使用 `task_create` 创建任务，再使用 `task_manage` 记录和维护任务进度。根据用户需求选择合适的能力检查、操作和验证设备状态。"
	nexusServerInstructions = "优先调用 `agentdock_context` 获取可用于操作用户设备的核心能力、Skill、动态 MCP、Workflow 模板、重要上下文和长期记忆索引。操作具体项目、切换工作区或工作区规则可能变化时，先调用节点范围的 `workspace_context` 获取当前工作区上下文。需要查找或读取长期记忆时使用 `recall_*`；需要查找或使用 Workflow 模板时使用 `workflow_template_manage`；处理多步骤任务时先使用 `task_create` 创建任务，再使用 `task_manage` 记录和维护任务进度。根据用户需求选择合适的能力检查、操作和验证设备状态。"
)

func serverInstructions(nexusEnabled bool) string {
	if nexusEnabled {
		return nexusServerInstructions
	}
	return baseServerInstructions
}
