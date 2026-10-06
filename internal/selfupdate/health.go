package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/updateengine"
)

type healthResponse struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
}

func healthCandidates(_ string) []string {
	var candidates []string
	if port, err := strconv.Atoi(strings.TrimSpace(os.Getenv("AGENTDOCK_PORT"))); err == nil && port > 0 && port <= 65535 {
		candidates = append(candidates, localHealthURL(os.Getenv("AGENTDOCK_HOST"), port))
	}
	candidates = append(candidates, "http://127.0.0.1:8765/healthz")
	return uniqueStrings(candidates)
}

func localHealthURL(host string, port int) string {
	host = strings.TrimSpace(host)
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("http://%s:%d/healthz", host, port)
}

func findHealthyURL(ctx context.Context, candidates []string) string {
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	for _, candidate := range candidates {
		response, err := readHealth(ctx, client, candidate)
		if err == nil && response.OK {
			return candidate
		}
	}
	return ""
}

func waitForVersion(ctx context.Context, candidates []string, targetVersion string, timeout time.Duration) error {
	return updateengine.WaitForVersion(ctx, candidates, targetVersion, timeout)
}

func readHealth(ctx context.Context, client *http.Client, endpoint string) (healthResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return healthResponse{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return healthResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return healthResponse{}, fmt.Errorf("%s 返回 HTTP %d", endpoint, resp.StatusCode)
	}
	var health healthResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&health); err != nil {
		return healthResponse{}, fmt.Errorf("解析 %s 失败: %w", endpoint, err)
	}
	return health, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
