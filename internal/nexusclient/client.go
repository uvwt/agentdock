package nexusclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	contracts "github.com/uvwt/agentdock/generated/nexuscontracts"
)

var (
	ErrUnauthorized = errors.New("nexus unauthorized")
	ErrTokenRevoked = errors.New("nexus device token revoked")
	ErrLeaseExpired = errors.New("nexus command lease expired")
)

type Config struct {
	BaseURL        string
	RequestTimeout time.Duration
	PollTimeout    time.Duration
	UserAgent      string
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	pollClient *http.Client
	userAgent  string
}

func New(cfg Config) (*Client, error) {
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid nexus base URL %q", cfg.BaseURL)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, errors.New("nexus base URL must use http or https")
	}
	requestTimeout := cfg.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = 15 * time.Second
	}
	pollTimeout := cfg.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = 35 * time.Second
	}
	return &Client{baseURL: baseURL, httpClient: &http.Client{Timeout: requestTimeout}, pollClient: &http.Client{Timeout: pollTimeout + 5*time.Second}, userAgent: cfg.UserAgent}, nil
}

func (c *Client) Enroll(ctx context.Context, request contracts.DeviceEnrollmentRequest) (contracts.DeviceEnrollmentResponse, error) {
	var response contracts.DeviceEnrollmentResponse
	err := c.doJSON(ctx, c.httpClient, http.MethodPost, "/v1/devices/enroll", "", request, &response)
	return response, err
}

func (c *Client) Heartbeat(ctx context.Context, state DeviceState, heartbeat contracts.DeviceHeartbeat) error {
	return c.doJSON(ctx, c.httpClient, http.MethodPost, devicePath(state.DeviceID, "heartbeat"), state.DeviceToken, heartbeat, nil)
}

func (c *Client) PollCommand(ctx context.Context, state DeviceState) (*contracts.CommandLease, error) {
	var lease contracts.CommandLease
	status, err := c.doJSONStatus(ctx, c.pollClient, http.MethodPost, devicePath(state.DeviceID, "commands", "lease"), state.DeviceToken, nil, &lease)
	if status == http.StatusNoContent {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &lease, nil
}

func (c *Client) ReportProgress(ctx context.Context, state DeviceState, commandID string, progress contracts.CommandProgress) error {
	return c.doJSON(ctx, c.httpClient, http.MethodPost, commandPath(commandID, "progress"), state.DeviceToken, progress, nil)
}

func (c *Client) CompleteCommand(ctx context.Context, state DeviceState, commandID string, result contracts.CommandResult) error {
	return c.doJSON(ctx, c.httpClient, http.MethodPost, commandPath(commandID, "result"), state.DeviceToken, result, nil)
}

func (c *Client) doJSON(ctx context.Context, client *http.Client, method, endpoint, token string, input, output any) error {
	_, err := c.doJSONStatus(ctx, client, method, endpoint, token, input, output)
	return err
}

func (c *Client) doJSONStatus(ctx context.Context, client *http.Client, method, endpoint, token string, input, output any) (int, error) {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return 0, fmt.Errorf("encode nexus request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	requestURL := *c.baseURL
	requestURL.Path = path.Join(strings.TrimSuffix(c.baseURL.Path, "/"), endpoint)
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("nexus request failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("read nexus response: %w", err)
	}
	if resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, decodeAPIError(resp.StatusCode, responseBody)
	}
	if output != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, output); err != nil {
			return resp.StatusCode, fmt.Errorf("decode nexus response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

func decodeAPIError(status int, body []byte) error {
	var apiError contracts.ErrorResponse
	_ = json.Unmarshal(body, &apiError)
	switch {
	case apiError.Code == "TOKEN_REVOKED":
		return ErrTokenRevoked
	case apiError.Code == "LEASE_EXPIRED":
		return ErrLeaseExpired
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return ErrUnauthorized
	}
	message := apiError.Message
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		message = http.StatusText(status)
	}
	return fmt.Errorf("nexus HTTP %d: %s", status, message)
}

func devicePath(deviceID string, segments ...string) string {
	parts := append([]string{"v1", "devices", url.PathEscape(deviceID)}, segments...)
	return "/" + path.Join(parts...)
}

func commandPath(commandID string, segment string) string {
	return "/" + path.Join("v1", "commands", url.PathEscape(commandID), segment)
}
