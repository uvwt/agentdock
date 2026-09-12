package installer

// CLI LaunchAgent 标签必须和 Darwin 运行时在“非 App Bundle”路径上读取的名字一致。
// AgentDock.app / SMAppService 使用 com.uvwt.agentdock.core 与 .tunnel，不能写进 CLI 安装器。
const (
	darwinCLICoreLabel   = "com.uvwt.agentdock"
	darwinCLITunnelLabel = "com.uvwt.agentdock.cloudflared"
)
