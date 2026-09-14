package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func waitHealthy(ctx context.Context, endpoint string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	var lastError error
	successes := 0
	for time.Now().Before(deadline) {
		ok, err := probeHealth(ctx, client, endpoint)
		if err != nil || !ok {
			successes = 0
			if err != nil {
				lastError = err
			} else {
				lastError = fmt.Errorf("%s 返回 ok=false", endpoint)
			}
		} else {
			successes++
			if successes >= 2 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if lastError == nil {
		lastError = fmt.Errorf("health check timed out: %s", endpoint)
	}
	return lastError
}

func probeHealth(ctx context.Context, client *http.Client, endpoint string) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%s returned HTTP %d", endpoint, response.StatusCode)
	}
	var body struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&body); err != nil {
		return false, err
	}
	return body.OK, nil
}
