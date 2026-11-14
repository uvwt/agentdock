package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	"github.com/uvwt/agentdock/internal/mcp/oauthclient"
	processcontrol "github.com/uvwt/agentdock/internal/process"
)

// go-sdk 公开 jsonrpc 包未导出 ErrRejected，只能通过稳定的 wire code 识别。
const sdkTransportRejectedCode int64 = -32005

// mcpSessionCloseTimeout 只约束会话终止的 HTTP DELETE。
// SDK 用脱离工具调用超时的连接上下文发这个请求，并且要等它返回后才取消连接；
// 对端不响应时必须由这里截止，避免清理一直占着服务器状态锁。
const mcpSessionCloseTimeout = 5 * time.Second

type protocolClient interface {
	initialize(context.Context) error
	listTools(context.Context) ([]Tool, error)
	callTool(context.Context, string, map[string]any) (map[string]any, error)
	close() error
}

type sdkProtocolClient struct {
	cfg        ServerConfig
	session    *mcpsdk.ClientSession
	command    *exec.Cmd
	controller *processcontrol.Controller
	stderr     *tailBuffer
	oauth      sdkauth.OAuthHandler
	closeOnce  sync.Once
	closeErr   error
}

func newStreamableHTTPClient(cfg ServerConfig, oauth sdkauth.OAuthHandler) *sdkProtocolClient {
	return &sdkProtocolClient{cfg: cfg, oauth: oauth}
}

func newStdioClient(cfg ServerConfig) *sdkProtocolClient {
	return &sdkProtocolClient{cfg: cfg, stderr: newTailBuffer(64 << 10)}
}

func (c *sdkProtocolClient) initialize(ctx context.Context) error {
	transport, err := c.transport()
	if err != nil {
		return err
	}
	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: config.ServerName, Version: buildinfo.Version},
		&mcpsdk.ClientOptions{Capabilities: &mcpsdk.ClientCapabilities{}},
	)
	// 新版 go-sdk 会在 ListTools 返回前静默过滤掉 nil 工具项，导致畸形
	// tools/list 响应被吞掉；在发送中间件中抢先检查原始响应并收敛为协议错误。
	client.AddSendingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil {
				return res, err
			}
			if listRes, ok := res.(*mcpsdk.ListToolsResult); ok && listRes != nil {
				for _, tool := range listRes.Tools {
					if tool == nil {
						return nil, newError("MCP_INVALID_RESPONSE", "MCP tools/list returned a null tool", false, map[string]any{"server": c.cfg.Name}, nil)
					}
				}
			}
			return res, nil
		}
	})
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		_ = c.cleanupProcess()
		return c.wrapSDKError("initialize MCP session", err)
	}
	initialized := session.InitializeResult()
	if initialized == nil || strings.TrimSpace(initialized.ProtocolVersion) == "" {
		_ = session.Close()
		_ = c.cleanupProcess()
		return newError("MCP_INVALID_RESPONSE", "MCP initialize response omitted protocolVersion", false, map[string]any{"server": c.cfg.Name}, nil)
	}
	c.session = session
	return nil
}

func (c *sdkProtocolClient) transport() (mcpsdk.Transport, error) {
	switch c.cfg.Transport {
	case TransportStreamableHTTP:
		headers, err := resolveHTTPHeaders(c.cfg)
		if err != nil {
			return nil, err
		}
		oauth := c.oauth
		if strings.TrimSpace(headers.Get("Authorization")) != "" {
			// 显式 Authorization Header 始终优先于自动 OAuth；否则 SDK 即使拿到
			// access token，也会在真正发请求前被静态 Header 覆盖。
			oauth = nil
		}
		return &mcpsdk.StreamableClientTransport{
			Endpoint:             c.cfg.URL,
			HTTPClient:           &http.Client{Transport: headerRoundTripper{headers: headers}, CheckRedirect: checkMCPRedirect},
			OAuthHandler:         oauth,
			MaxRetries:           -1,
			DisableStandaloneSSE: true,
		}, nil
	case TransportStdio:
		return c.startStdioTransport()
	default:
		return nil, newError("MCP_TRANSPORT_UNSUPPORTED", fmt.Sprintf("unsupported MCP transport %q", c.cfg.Transport), false, map[string]any{"server": c.cfg.Name}, nil)
	}
}

func (c *sdkProtocolClient) startStdioTransport() (mcpsdk.Transport, error) {
	environment, err := stdioEnvironment(c.cfg)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
	cmd.Dir = c.cfg.Cwd
	cmd.Env = environment
	cmd.Stderr = c.stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, newError("MCP_START_FAILED", "open MCP stdio stdout", false, map[string]any{"server": c.cfg.Name}, err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, newError("MCP_START_FAILED", "open MCP stdio stdin", false, map[string]any{"server": c.cfg.Name}, err)
	}
	processcontrol.Configure(cmd)
	if err := cmd.Start(); err != nil {
		return nil, newError("MCP_START_FAILED", "start MCP stdio server", false, map[string]any{"server": c.cfg.Name, "command": c.cfg.Command}, err)
	}
	controller, err := processcontrol.Attach(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, newError("MCP_START_FAILED", "attach MCP stdio process controller", false, map[string]any{"server": c.cfg.Name}, err)
	}
	c.command = cmd
	c.controller = controller
	return &mcpsdk.IOTransport{Reader: stdout, Writer: stdin}, nil
}

func (c *sdkProtocolClient) listTools(ctx context.Context) (tools []Tool, err error) {
	// SDK 会在返回工具前处理参数注解；畸形列表（如 null 元素）可能在
	// 其内部 panic。外部协议解码失败在此收敛为错误，不能击穿授权后的后台刷新。
	defer func() {
		if recover() != nil {
			tools = nil
			err = newError("MCP_INVALID_RESPONSE", "MCP tools/list returned an invalid tool definition", false, map[string]any{"server": c.cfg.Name}, nil)
		}
	}()
	if c.session == nil {
		return nil, newError("MCP_CONNECTION_FAILED", "MCP session is not initialized", true, map[string]any{"server": c.cfg.Name}, nil)
	}
	tools = make([]Tool, 0)
	for remote, err := range c.session.Tools(ctx, nil) {
		if err != nil {
			return nil, c.wrapSDKError("list MCP tools", err)
		}
		tool, err := convertSDKTool(remote)
		if err != nil {
			return nil, newError("MCP_INVALID_RESPONSE", "decode MCP tool definition", false, map[string]any{"server": c.cfg.Name}, err)
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func (c *sdkProtocolClient) callTool(ctx context.Context, name string, arguments map[string]any) (map[string]any, error) {
	if c.session == nil {
		return nil, newError("MCP_CONNECTION_FAILED", "MCP session is not initialized", true, map[string]any{"server": c.cfg.Name}, nil)
	}
	result, err := c.session.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return nil, c.wrapSDKError("call MCP tool", err)
	}
	return jsonObject(result)
}

func (c *sdkProtocolClient) close() error {
	c.closeOnce.Do(func() {
		if c.session != nil {
			c.closeErr = errors.Join(c.closeErr, c.session.Close())
			c.session = nil
		}
		c.closeErr = errors.Join(c.closeErr, c.cleanupProcess())
	})
	return c.closeErr
}

func (c *sdkProtocolClient) cleanupProcess() error {
	var result error
	if c.controller != nil {
		// SDK 会先关闭 stdin 让服务自行退出；这里再收口整个进程树，避免
		// MCP Server 派生的子进程遗留在 AgentDock 生命周期之外。
		_ = c.controller.Terminate()
		result = errors.Join(result, c.controller.Close())
		c.controller = nil
	}
	if c.command != nil {
		if err := c.command.Wait(); err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) && !errors.Is(err, os.ErrProcessDone) {
				result = errors.Join(result, err)
			}
		}
		c.command = nil
	}
	return result
}

func (c *sdkProtocolClient) wrapSDKError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var mcpErr *Error
	if errors.As(err, &mcpErr) {
		return mcpErr
	}
	details := map[string]any{"server": c.cfg.Name}
	var authRequired *oauthclient.AuthRequiredError
	if errors.As(err, &authRequired) {
		return newError("MCP_AUTH_REQUIRED", authRequired.Error(), false, details, err)
	}
	if c.stderr != nil && c.stderr.String() != "" {
		details["stderr"] = c.stderr.String()
	}
	var rpcErr *sdkjsonrpc.Error
	if errors.As(err, &rpcErr) {
		details["rpc_code"] = rpcErr.Code
		if len(rpcErr.Data) > 0 {
			details["rpc_data"] = strings.TrimSpace(string(rpcErr.Data))
		}
		message := strings.TrimSpace(rpcErr.Message)
		if message == "" {
			message = fmt.Sprintf("MCP JSON-RPC error %d", rpcErr.Code)
		}
		// go-sdk 的 transport rejection 是请求级故障，并明确保留逻辑连接可用。
		if rpcErr.Code == sdkTransportRejectedCode {
			return newError("MCP_TRANSPORT_REJECTED", message, true, details, err)
		}
		return newError("MCP_REMOTE_ERROR", message, false, details, err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return newError("MCP_TIMEOUT", operation+" timed out", true, details, err)
	}
	return newError("MCP_CONNECTION_FAILED", operation, true, details, err)
}

func convertSDKTool(remote *mcpsdk.Tool) (Tool, error) {
	if remote == nil {
		return Tool{}, errors.New("MCP tools/list returned a null tool")
	}
	input, err := jsonMap(remote.InputSchema)
	if err != nil {
		return Tool{}, fmt.Errorf("decode inputSchema: %w", err)
	}
	output, err := jsonMap(remote.OutputSchema)
	if err != nil {
		return Tool{}, fmt.Errorf("decode outputSchema: %w", err)
	}
	annotations, err := jsonMap(remote.Annotations)
	if err != nil {
		return Tool{}, fmt.Errorf("decode annotations: %w", err)
	}
	return Tool{
		Name:         remote.Name,
		Title:        remote.Title,
		Description:  remote.Description,
		InputSchema:  input,
		OutputSchema: output,
		Annotations:  annotations,
	}, nil
}

func jsonMap(value any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	return jsonObject(value)
}

func jsonObject(value any) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func stdioEnvironment(cfg ServerConfig) ([]string, error) {
	environment := envstore.MinimalSystemEnv()
	for childName, hostName := range cfg.EnvFromEnv {
		value, ok := os.LookupEnv(hostName)
		if !ok {
			return nil, newError(
				"MCP_CREDENTIAL_REQUIRED",
				"required MCP stdio environment variable is missing",
				false,
				map[string]any{"server": cfg.Name, "env": hostName},
				nil,
			)
		}
		environment[childName] = value
	}
	// portable Plugin mcp.json 中的 env 是包内公开默认值。用户的
	// env/mcp/<storage-key>.env 以及宿主保留变量在 RuntimeEnv 中最后覆盖。
	for key, value := range cfg.StaticEnv {
		environment[key] = value
	}
	for key, value := range cfg.RuntimeEnv {
		environment[key] = value
	}
	return envstore.Format(environment), nil
}

func resolveHTTPHeaders(cfg ServerConfig) (http.Header, error) {
	headers := make(http.Header, len(cfg.StaticHeaders)+len(cfg.HeaderEnv)+1)
	for name, value := range cfg.StaticHeaders {
		headers.Set(name, value)
	}
	headers.Set("User-Agent", config.ServerName+"/"+buildinfo.Version)
	requiredEnv := make(map[string]struct{}, len(cfg.RequiredEnv))
	for _, envName := range cfg.RequiredEnv {
		requiredEnv[envName] = struct{}{}
	}
	for header, envName := range cfg.HeaderEnv {
		value, ok := cfg.RuntimeEnv[envName]
		if !ok && cfg.SourceType != "plugin" {
			value, ok = os.LookupEnv(envName)
		}
		if !ok || value == "" {
			if cfg.SourceType == "plugin" {
				if _, required := requiredEnv[envName]; !required {
					// optional HeaderEnv 缺失时不要发送空 Header。空 Authorization 会覆盖
					// OAuthHandler 生成的 Bearer token，也会改变匿名服务的请求语义。
					continue
				}
			}
			return nil, newError(
				"MCP_CREDENTIAL_REQUIRED",
				"required MCP HTTP header environment variable is missing",
				false,
				map[string]any{"server": cfg.Name, "header": header, "env": envName},
				nil,
			)
		}
		headers.Set(header, value)
	}
	return headers, nil
}

type headerRoundTripper struct {
	headers http.Header
}

func checkMCPRedirect(request *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("MCP HTTP redirect limit exceeded")
	}
	if len(via) > 0 && (!strings.EqualFold(request.URL.Scheme, via[0].URL.Scheme) || !strings.EqualFold(request.URL.Host, via[0].URL.Host)) {
		// 传输层会补入静态请求头；OAuth 也可能持有令牌。跨来源或协议降级
		// 必须在下一个 RoundTrip 之前拒绝，不能依赖标准库删除敏感头。
		return errors.New("MCP HTTP redirect to a different origin is not allowed")
	}
	return nil
}

type sessionRetirementKey struct{}

func contextWithSessionRetirement(ctx context.Context, retire func()) context.Context {
	if retire == nil {
		return ctx
	}
	return context.WithValue(ctx, sessionRetirementKey{}, retire)
}

func retireSessionFromContext(ctx context.Context) {
	retire, _ := ctx.Value(sessionRetirementKey{}).(func())
	if retire != nil {
		retire()
	}
}

// retiresMCPSession 报告这个 HTTP 状态会让 SDK 终止逻辑会话，并在返回调用方之前同步发 DELETE。
// 临时故障和认证挑战会保留或重试连接，不能提前拆掉缓存。
func retiresMCPSession(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return false
	default:
		return status >= 400
	}
}

func (t headerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	ctx := request.Context()
	if request.Method == http.MethodDelete {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, mcpSessionCloseTimeout)
			defer cancel()
		}
	}
	clone := request.Clone(ctx)
	clone.Header = request.Header.Clone()
	for name, values := range t.headers {
		clone.Header.Del(name)
		for _, value := range values {
			clone.Header.Add(name, value)
		}
	}
	response, err := http.DefaultTransport.RoundTrip(clone)
	if err != nil || response == nil || !retiresMCPSession(response.StatusCode) {
		return response, err
	}
	// 调用协程此刻持有服务器状态锁。必须在把响应交回 SDK 之前退出缓存并放开锁：
	// SDK 会在 CallTool 返回前同步发送 DELETE，对端不响应时不能继续占着这把锁。
	retireSessionFromContext(request.Context())
	return response, nil
}

type tailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func newTailBuffer(limit int) *tailBuffer {
	return &tailBuffer{limit: limit, data: make([]byte, 0, limit)}
}

func (b *tailBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(data)
	if len(data) >= b.limit {
		b.data = append(b.data[:0], data[len(data)-b.limit:]...)
		return original, nil
	}
	if overflow := len(b.data) + len(data) - b.limit; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
	}
	b.data = append(b.data, data...)
	return original, nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}
