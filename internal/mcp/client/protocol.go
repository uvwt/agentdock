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

	sdkjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/uvwt/agentdock/internal/buildinfo"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/envstore"
	processcontrol "github.com/uvwt/agentdock/internal/process"
)

// go-sdk 公开 jsonrpc 包未导出 ErrRejected，只能通过稳定的 wire code 识别。
const sdkTransportRejectedCode int64 = -32005

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
	closeOnce  sync.Once
	closeErr   error
}

func newStreamableHTTPClient(cfg ServerConfig) *sdkProtocolClient {
	return &sdkProtocolClient{cfg: cfg}
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
		return &mcpsdk.StreamableClientTransport{
			Endpoint:             c.cfg.URL,
			HTTPClient:           &http.Client{Transport: headerRoundTripper{headers: headers}},
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

func (c *sdkProtocolClient) listTools(ctx context.Context) ([]Tool, error) {
	if c.session == nil {
		return nil, newError("MCP_CONNECTION_FAILED", "MCP session is not initialized", true, map[string]any{"server": c.cfg.Name}, nil)
	}
	tools := make([]Tool, 0)
	for remote, err := range c.session.Tools(ctx, nil) {
		if err != nil {
			return nil, c.wrapSDKError("list MCP tools", err)
		}
		tool, err := convertSDKTool(remote)
		if err != nil {
			return nil, newError("MCP_INVALID_RESPONSE", "decode MCP tool definition", false, map[string]any{"server": c.cfg.Name, "tool": remote.Name}, err)
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
	details := map[string]any{"server": c.cfg.Name}
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
				"MCP_AUTH_REQUIRED",
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
	for header, envName := range cfg.HeaderEnv {
		value, ok := cfg.RuntimeEnv[envName]
		if !ok && cfg.SourceType != "plugin" {
			value, ok = os.LookupEnv(envName)
		}
		if !ok || value == "" {
			return nil, newError(
				"MCP_AUTH_REQUIRED",
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

func (t headerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	for name, values := range t.headers {
		clone.Header.Del(name)
		for _, value := range values {
			clone.Header.Add(name, value)
		}
	}
	return http.DefaultTransport.RoundTrip(clone)
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
