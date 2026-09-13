package desktopruntime

import (
	"fmt"
	"os"
	"path/filepath"
)

const isolatedCloudflaredConfig = "{}\n"

// prepareCloudflaredTunnelArgs 强制 cloudflared 使用 AgentDock 自己的最小配置，避免
// ~/.cloudflared/config.yml 中的 ingress 等用户配置意外改变 AgentDock Tunnel 行为。
// 使用有效的空 YAML 对象而不是零字节文件，避免 cloudflared 把正常启动记录为配置错误。
func prepareCloudflaredTunnelArgs(root string, trailing ...string) ([]string, error) {
	configPath := filepath.Join(root, "cloudflared-isolated.yml")
	if err := os.WriteFile(configPath, []byte(isolatedCloudflaredConfig), 0o600); err != nil {
		return nil, fmt.Errorf("写入 cloudflared 隔离配置失败: %w", err)
	}
	arguments := []string{"--config", configPath, "tunnel", "--no-autoupdate"}
	return append(arguments, trailing...), nil
}
