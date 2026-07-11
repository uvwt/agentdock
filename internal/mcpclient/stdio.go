package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/processcontrol"
)

const maxStdioMessageBytes = 32 << 20

type stdioClient struct {
	cfg        ServerConfig
	mu         sync.Mutex
	cmd        *exec.Cmd
	controller *processcontrol.Controller
	stdin      io.WriteCloser
	stdout     *bufio.Reader
	stderr     *tailBuffer
	nextID     int64
	closed     bool
}

func newStdioClient(cfg ServerConfig) *stdioClient {
	return &stdioClient{cfg: cfg, stderr: newTailBuffer(64 << 10)}
}

func (c *stdioClient) initialize(ctx context.Context) error {
	if err := c.start(); err != nil {
		return err
	}
	var result initializeResult
	if err := c.request(ctx, rpcRequest{
		JSONRPC: "2.0",
		ID:      c.requestID(),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": config.ProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": config.ServerName, "version": config.Version},
		},
	}, &result); err != nil {
		return err
	}
	if strings.TrimSpace(result.ProtocolVersion) == "" {
		return newError("MCP_INVALID_RESPONSE", "MCP initialize response omitted protocolVersion", false, nil, nil)
	}
	return c.notify(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized", Params: map[string]any{}})
}

func (c *stdioClient) listTools(ctx context.Context) ([]Tool, error) {
	tools := make([]Tool, 0)
	cursor := ""
	for page := 0; page < 100; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result listToolsResult
		if err := c.request(ctx, rpcRequest{JSONRPC: "2.0", ID: c.requestID(), Method: "tools/list", Params: params}, &result); err != nil {
			return nil, err
		}
		tools = append(tools, result.Tools...)
		if result.NextCursor == "" {
			return tools, nil
		}
		cursor = result.NextCursor
	}
	return nil, newError("MCP_INVALID_RESPONSE", "MCP tools/list pagination exceeded 100 pages", false, nil, nil)
}

func (c *stdioClient) callTool(ctx context.Context, name string, arguments map[string]any) (map[string]any, error) {
	var result map[string]any
	if err := c.request(ctx, rpcRequest{
		JSONRPC: "2.0",
		ID:      c.requestID(),
		Method:  "tools/call",
		Params:  map[string]any{"name": name, "arguments": arguments},
	}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *stdioClient) start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil && !c.closed {
		return nil
	}
	cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
	cmd.Dir = c.cfg.Cwd
	cmd.Env = os.Environ()
	for childName, hostName := range c.cfg.EnvFromEnv {
		value, ok := os.LookupEnv(hostName)
		if !ok {
			return newError(
				"MCP_AUTH_REQUIRED",
				"required MCP stdio environment variable is missing",
				false,
				map[string]any{"server": c.cfg.Name, "env": hostName},
				nil,
			)
		}
		cmd.Env = append(cmd.Env, childName+"="+value)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return newError("MCP_START_FAILED", "open MCP stdio stdin", false, nil, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return newError("MCP_START_FAILED", "open MCP stdio stdout", false, nil, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return newError("MCP_START_FAILED", "open MCP stdio stderr", false, nil, err)
	}
	processcontrol.Configure(cmd)
	if err := cmd.Start(); err != nil {
		return newError("MCP_START_FAILED", "start MCP stdio server", false, map[string]any{"server": c.cfg.Name, "command": c.cfg.Command}, err)
	}
	controller, err := processcontrol.Attach(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return newError("MCP_START_FAILED", "attach MCP stdio process controller", false, nil, err)
	}
	c.cmd = cmd
	c.controller = controller
	c.stdin = stdin
	c.stdout = bufio.NewReaderSize(stdout, 64<<10)
	c.closed = false
	go func() {
		_, _ = io.Copy(c.stderr, stderr)
	}()
	return nil
}

func (c *stdioClient) request(ctx context.Context, request rpcRequest, output any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil || c.closed {
		return newError("MCP_CONNECTION_FAILED", "MCP stdio server is not running", true, map[string]any{"server": c.cfg.Name}, nil)
	}
	if err := c.writeLocked(request); err != nil {
		return err
	}
	type readResult struct {
		response rpcResponse
		err      error
	}
	resultCh := make(chan readResult, 1)
	go func() {
		response, err := c.readResponseLocked()
		resultCh <- readResult{response: response, err: err}
	}()
	select {
	case <-ctx.Done():
		c.terminateLocked()
		return newError("MCP_TIMEOUT", "MCP stdio request timed out", true, map[string]any{"server": c.cfg.Name}, ctx.Err())
	case result := <-resultCh:
		if result.err != nil {
			return result.err
		}
		return decodeRPCResult(result.response, output)
	}
}

func (c *stdioClient) notify(request rpcRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd == nil || c.closed {
		return newError("MCP_CONNECTION_FAILED", "MCP stdio server is not running", true, map[string]any{"server": c.cfg.Name}, nil)
	}
	return c.writeLocked(request)
}

func (c *stdioClient) writeLocked(request rpcRequest) error {
	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode MCP stdio request: %w", err)
	}
	data = append(data, '\n')
	if _, err := c.stdin.Write(data); err != nil {
		return newError("MCP_CONNECTION_FAILED", "write MCP stdio request", true, map[string]any{"server": c.cfg.Name, "stderr": c.stderr.String()}, err)
	}
	return nil
}

func (c *stdioClient) readResponseLocked() (rpcResponse, error) {
	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return rpcResponse{}, newError("MCP_CONNECTION_FAILED", "read MCP stdio response", true, map[string]any{"server": c.cfg.Name, "stderr": c.stderr.String()}, err)
		}
		if len(line) > maxStdioMessageBytes {
			return rpcResponse{}, newError("MCP_RESPONSE_TOO_LARGE", "MCP stdio response exceeds 32 MiB", false, nil, nil)
		}
		line = bytesTrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var response rpcResponse
		if err := json.Unmarshal(line, &response); err != nil {
			return rpcResponse{}, newError("MCP_INVALID_RESPONSE", "decode MCP stdio JSON-RPC response", false, map[string]any{"stderr": c.stderr.String()}, err)
		}
		if len(response.ID) == 0 && response.Error == nil && len(response.Result) == 0 {
			continue
		}
		return response, nil
	}
}

func (c *stdioClient) requestID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	return c.nextID
}

func (c *stdioClient) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.terminateLocked()
}

func (c *stdioClient) terminateLocked() error {
	if c.closed {
		return nil
	}
	c.closed = true
	var result error
	if c.stdin != nil {
		if err := c.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			result = errors.Join(result, err)
		}
	}
	if c.controller != nil {
		// 主动关闭持久 MCP 进程时，子进程被信号终止属于预期生命周期，
		// 这里只保留 Job Object / 进程组句柄释放失败。
		_ = c.controller.Terminate()
		result = errors.Join(result, c.controller.Close())
	} else if c.cmd != nil && c.cmd.Process != nil {
		if err := c.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			result = errors.Join(result, err)
		}
	}
	if c.cmd != nil {
		if err := c.cmd.Wait(); err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) && !errors.Is(err, os.ErrProcessDone) {
				result = errors.Join(result, err)
			}
		}
	}
	return result
}

func bytesTrimSpace(input []byte) []byte {
	return []byte(strings.TrimSpace(string(input)))
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
