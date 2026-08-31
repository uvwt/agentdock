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
	return Client{
		endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		token:    strings.TrimSpace(token),
		httpClient: http.Client{
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
	if body != nil {
		reader = bytes.NewReader(body)
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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
