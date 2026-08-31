package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestDockerSmokeUsesStreamableHTTPAcceptHeader(t *testing.T) {
	data, err := os.ReadFile("../../packaging/docker/smoke-docker.sh")
	if err != nil {
		t.Fatalf("read smoke-docker.sh: %v", err)
	}
	const streamableHTTPAccept = `if path == "/mcp":
        headers["accept"] = "application/json, text/event-stream"`
	const optionalIsError = `envelope.get("isError", False) is False`

	// actions/checkout 在 Windows Runner 上可能把 shell 脚本检出为 CRLF；
	// 同时验证两种换行，测试协议语义而不是平台文本格式。
	lfScript := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, test := range []struct {
		name   string
		script string
	}{
		{name: "LF", script: lfScript},
		{name: "CRLF", script: strings.ReplaceAll(lfScript, "\n", "\r\n")},
	} {
		t.Run(test.name, func(t *testing.T) {
			script := strings.ReplaceAll(test.script, "\r\n", "\n")
			if !strings.Contains(script, streamableHTTPAccept) {
				t.Fatal("smoke-docker.sh must send the Streamable HTTP Accept header for MCP requests")
			}
			if !strings.Contains(script, optionalIsError) {
				t.Fatal("smoke-docker.sh must treat an omitted MCP isError field as success")
			}
		})
	}
}
func TestCloudflareComposeKeepsTunnelTokenOutOfAgentDock(t *testing.T) {
	data, err := os.ReadFile("../../docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	compose := string(data)
	for _, want := range []string{
		`profiles: ["cloudflare-quick"]`,
		`profiles: ["cloudflare-named"]`,
		`http://agentdock:8765`,
		`TUNNEL_TOKEN: "${TUNNEL_TOKEN:-}"`,
		`${AGENTDOCK_PUBLISH_PORT:-8765}:8765`,
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("docker-compose.yml missing %q", want)
		}
	}
	// TUNNEL_TOKEN 环境变量只应挂在 cloudflared-named，不能注入 agentdock 主服务。
	// 用 "KEY: " 赋值形态判断，避免中文注释里提到同名变量时误报。
	agentStart := strings.Index(compose, "\n  agentdock:")
	if agentStart < 0 {
		agentStart = strings.Index(compose, "  agentdock:")
	}
	namedStart := strings.Index(compose, "\n  cloudflared-named:")
	if namedStart < 0 {
		namedStart = strings.Index(compose, "  cloudflared-named:")
	}
	if agentStart < 0 || namedStart < 0 || namedStart <= agentStart {
		t.Fatal("unexpected docker-compose.yml service layout")
	}
	if strings.Contains(compose[agentStart:namedStart], "TUNNEL_TOKEN:") {
		t.Fatal("agentdock service must not receive TUNNEL_TOKEN")
	}
}
