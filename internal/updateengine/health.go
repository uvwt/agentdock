package updateengine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type HealthResponse struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
}

func WaitForVersion(ctx context.Context, endpoints []string, targetVersion string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	consecutive := make(map[string]int, len(endpoints))
	var lastError error
	for time.Now().Before(deadline) {
		for _, endpoint := range endpoints {
			endpoint = strings.TrimSpace(endpoint)
			if endpoint == "" {
				continue
			}
			health, err := readHealth(ctx, client, endpoint)
			if err != nil {
				consecutive[endpoint] = 0
				lastError = err
				continue
			}
			if !health.OK || NormalizeVersion(health.Version) != NormalizeVersion(targetVersion) {
				consecutive[endpoint] = 0
				lastError = fmt.Errorf("%s reports version %s (ok=%t), target is %s", endpoint, health.Version, health.OK, NormalizeVersion(targetVersion))
				continue
			}
			consecutive[endpoint]++
			if consecutive[endpoint] >= 2 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if lastError == nil {
		lastError = errorsNoHealthEndpoint{}
	}
	return lastError
}

type errorsNoHealthEndpoint struct{}

func (errorsNoHealthEndpoint) Error() string { return "no healthy AgentDock health endpoint" }

func readHealth(ctx context.Context, client *http.Client, endpoint string) (HealthResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return HealthResponse{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return HealthResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return HealthResponse{}, fmt.Errorf("%s returned HTTP %d", endpoint, response.StatusCode)
	}
	var health HealthResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&health); err != nil {
		return HealthResponse{}, fmt.Errorf("parse %s: %w", endpoint, err)
	}
	return health, nil
}
