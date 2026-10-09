package nexusclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var ErrResponseTooLarge = errors.New("Nexus response exceeds configured limit")

// Client 只负责 AgentDock 到已配对 NexusDock 的通用 HTTP 传输策略。
// 领域路径、超时、响应结构和错误语义继续由调用方拥有。
type Client struct {
	endpoint   string
	token      string
	httpClient http.Client
}

func New(endpoint, token string) Client {
	return newClient(endpoint, token, nil)
}

// NewWithTransport 只替换这一次调用的传输层。
// 传 nil 时与 New 相同，仍使用默认 Transport，包括系统环境代理。
// 资源库的固定拨号通过这里接入，避免改掉其他 Nexus HTTP 调用的行为。
func NewWithTransport(endpoint, token string, transport http.RoundTripper) Client {
	return newClient(endpoint, token, transport)
}

func newClient(endpoint, token string, transport http.RoundTripper) Client {
	return Client{
		endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		token:    strings.TrimSpace(token),
		httpClient: http.Client{
			Transport: transport,
			// Nexus Device Token 不应跨重定向传播；重定向由领域调用方作为普通 HTTP 响应处理。
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (c Client) Endpoint() string {
	return c.endpoint
}

func (c Client) Do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var reader io.Reader
	var length int64
	contentType := ""
	if body != nil {
		reader = bytes.NewReader(body)
		length = int64(len(body))
		contentType = "application/json"
	}
	return c.do(ctx, method, path, reader, length, contentType)
}

// DoStream 按调用方给出的长度发送正文，不把正文读进内存。
// Content-Length 固定为 contentLength，避免大 ZIP 被改成 chunked，也避免 Device Token 请求在重定向前被缓冲。
func (c Client) DoStream(ctx context.Context, method, path, contentType string, body io.Reader, contentLength int64) (*http.Response, error) {
	if body == nil || contentLength < 0 || strings.TrimSpace(contentType) == "" {
		return nil, fmt.Errorf("Nexus stream request requires a body, content type, and content length")
	}
	return c.do(ctx, method, path, body, contentLength, contentType)
}

func (c Client) do(ctx context.Context, method, path string, body io.Reader, contentLength int64, contentType string) (*http.Response, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.ContentLength = contentLength
		req.Header.Set("Content-Type", contentType)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.httpClient.Do(req)
}

func ReadBoundedBody(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("Nexus response body limit must be positive")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrResponseTooLarge, maxBytes)
	}
	return data, nil
}
