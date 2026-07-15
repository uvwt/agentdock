package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type healthResponse struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
}

func healthCandidates(targetPath string) []string {
	var candidates []string
	if port, err := strconv.Atoi(strings.TrimSpace(os.Getenv("AGENTDOCK_PORT"))); err == nil && port > 0 && port <= 65535 {
		candidates = append(candidates, fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
	}

	// 当前 macOS 裸机部署把端口写在启动脚本中；读取它可以避免把本机端口硬编码进更新流程。
	if home, err := os.UserHomeDir(); err == nil {
		startScript := filepath.Join(home, "agentdock-runtime", "start-agentdock.sh")
		if data, readErr := os.ReadFile(startScript); readErr == nil && strings.Contains(string(data), targetPath) {
			portPattern := regexp.MustCompile(`--port\s+([0-9]+)`)
			if match := portPattern.FindStringSubmatch(string(data)); len(match) == 2 {
				candidates = append(candidates, "http://127.0.0.1:"+match[1]+"/healthz")
			}
		}
	}
	candidates = append(candidates, "http://127.0.0.1:8765/healthz")
	return uniqueStrings(candidates)
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
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	var lastError error
	consecutiveSuccesses := make(map[string]int, len(candidates))
	for time.Now().Before(deadline) {
		for _, candidate := range candidates {
			response, err := readHealth(ctx, client, candidate)
			if err != nil {
				consecutiveSuccesses[candidate] = 0
				lastError = err
				continue
			}
			if !response.OK {
				consecutiveSuccesses[candidate] = 0
				lastError = fmt.Errorf("%s 返回 ok=false", candidate)
				continue
			}
			if normalizeVersion(response.Version) != normalizeVersion(targetVersion) {
				consecutiveSuccesses[candidate] = 0
				lastError = fmt.Errorf("%s 运行版本为 %s，目标版本为 %s", candidate, response.Version, targetVersion)
				continue
			}
			consecutiveSuccesses[candidate]++
			if consecutiveSuccesses[candidate] >= 2 {
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
		lastError = fmt.Errorf("没有可用的 healthz 地址")
	}
	return lastError
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
