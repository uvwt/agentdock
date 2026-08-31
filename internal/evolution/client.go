package evolution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/nexusclient"
)

const maxNexusResponseBytes = 4 << 20

var ErrRevisionConflict = errors.New("evolution revision conflict")

type client struct {
	config func() config.Config
}

func (c client) query(ctx context.Context, query Query) ([]Record, error) {
	var out queryResult
	if err := c.post(ctx, "/internal/recall/lifecycle/query", query, &out); err != nil {
		return nil, err
	}
	return out.Records, nil
}

func (c client) transition(ctx context.Context, request transitionRequest) (transitionResult, error) {
	var out transitionResult
	if err := c.post(ctx, "/internal/recall/lifecycle/transition", request, &out); err != nil {
		return transitionResult{}, err
	}
	return out, nil
}

func (c client) post(ctx context.Context, path string, payload any, out any) error {
	cfg := c.config()
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.NexusEndpoint), "/")
	if endpoint == "" {
		return errors.New("AgentDock is not paired with NexusDock")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(cfg.NexusDeviceToken)
	if token == "" {
		return errors.New("NexusDock Device Token is unavailable; pair this AgentDock device first")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	client := nexusclient.New(endpoint, token)
	resp, err := client.Do(requestCtx, http.MethodPost, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := nexusclient.ReadBoundedBody(resp.Body, maxNexusResponseBytes)
	if err != nil {
		if errors.Is(err, nexusclient.ErrResponseTooLarge) {
			return errors.New("Nexus lifecycle response exceeds 4 MiB")
		}
		return fmt.Errorf("read Nexus lifecycle response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &failure)
		if resp.StatusCode == http.StatusConflict && failure.Code == "LIFECYCLE_REVISION_CONFLICT" {
			return ErrRevisionConflict
		}
		return fmt.Errorf("Nexus lifecycle request failed: %s", firstNonEmpty(failure.Error, resp.Status))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode Nexus lifecycle response: %w", err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "unknown error"
}
