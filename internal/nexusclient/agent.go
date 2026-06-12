package nexusclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	contracts "github.com/uvwt/agentdock/generated/nexuscontracts"
	"github.com/uvwt/agentdock/internal/commandqueue"
)

type AgentConfig struct {
	HeartbeatInterval time.Duration
	OfflineBackoffMin time.Duration
	OfflineBackoffMax time.Duration
	OutboxBatchSize   int
}

func (c *AgentConfig) normalize() {
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 30 * time.Second
	}
	if c.OfflineBackoffMin <= 0 {
		c.OfflineBackoffMin = time.Second
	}
	if c.OfflineBackoffMax < c.OfflineBackoffMin {
		c.OfflineBackoffMax = 30 * time.Second
	}
	if c.OutboxBatchSize <= 0 {
		c.OutboxBatchSize = 50
	}
}

type Agent struct {
	client    *Client
	state     *StateStore
	outbox    *commandqueue.Outbox
	executor  *commandqueue.Executor
	heartbeat HeartbeatProvider
	config    AgentConfig
	now       func() time.Time

	mu          sync.RWMutex
	deviceState DeviceState
}

func NewAgent(client *Client, state *StateStore, outbox *commandqueue.Outbox, executor *commandqueue.Executor, heartbeat HeartbeatProvider, cfg AgentConfig) (*Agent, error) {
	if client == nil || state == nil || outbox == nil || executor == nil || heartbeat == nil {
		return nil, errors.New("nexus client, state, outbox, executor, and heartbeat provider are required")
	}
	cfg.normalize()
	return &Agent{client: client, state: state, outbox: outbox, executor: executor, heartbeat: heartbeat, config: cfg, now: time.Now}, nil
}

func (a *Agent) Enroll(ctx context.Context, request contracts.DeviceEnrollmentRequest) (DeviceState, error) {
	response, err := a.client.Enroll(ctx, request)
	if err != nil {
		return DeviceState{}, err
	}
	if response.DeviceId == "" || response.DeviceToken == "" {
		return DeviceState{}, errors.New("nexus enrollment response omitted device credentials")
	}
	var expiresAt *time.Time
	if response.TokenExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, response.TokenExpiresAt)
		if err != nil {
			return DeviceState{}, fmt.Errorf("invalid token_expires_at: %w", err)
		}
		expiresAt = &parsed
	}
	now := a.now().UTC()
	state := DeviceState{DeviceID: response.DeviceId, DeviceToken: response.DeviceToken, TokenExpiresAt: expiresAt, EnrolledAt: now, UpdatedAt: now}
	if err := a.state.Save(state); err != nil {
		return DeviceState{}, err
	}
	a.setDeviceState(state)
	if response.HeartbeatIntervalSeconds > 0 {
		a.config.HeartbeatInterval = time.Duration(response.HeartbeatIntervalSeconds) * time.Second
	}
	return state, nil
}

func (a *Agent) Run(ctx context.Context) error {
	state, err := a.state.Load()
	if err != nil {
		return err
	}
	if !state.Valid(a.now()) {
		return errors.New("valid device enrollment is required")
	}
	a.setDeviceState(state)

	heartbeatTicker := time.NewTicker(a.config.HeartbeatInterval)
	defer heartbeatTicker.Stop()
	a.sendHeartbeat(ctx)
	a.drainOutbox(ctx)

	backoff := a.config.OfflineBackoffMin
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		select {
		case <-heartbeatTicker.C:
			a.sendHeartbeat(ctx)
		default:
		}

		state = a.getDeviceState()
		lease, err := a.client.PollCommand(ctx, state)
		if err != nil {
			if stopErr := a.handleCommunicationError(err); stopErr != nil {
				return stopErr
			}
			if !sleepContext(ctx, jitter(backoff)) {
				return nil
			}
			backoff *= 2
			if backoff > a.config.OfflineBackoffMax {
				backoff = a.config.OfflineBackoffMax
			}
			continue
		}
		backoff = a.config.OfflineBackoffMin
		a.drainOutbox(ctx)
		if lease == nil {
			if !sleepContext(ctx, a.config.OfflineBackoffMin) {
				return nil
			}
			continue
		}
		if err := a.executeLease(ctx, *lease); err != nil {
			if stopErr := a.handleCommunicationError(err); stopErr != nil {
				return stopErr
			}
		}
	}
}

func (a *Agent) sendHeartbeat(ctx context.Context) {
	state := a.getDeviceState()
	heartbeat := a.heartbeat.Heartbeat(state.DeviceID)
	if err := a.client.Heartbeat(ctx, state, heartbeat); err != nil {
		_, _ = a.outbox.Put(commandqueue.OutboxHeartbeat, "latest", heartbeat)
	}
}

func (a *Agent) executeLease(ctx context.Context, lease contracts.CommandLease) error {
	state := a.getDeviceState()
	if err := a.client.StartCommand(ctx, state, lease.Command.Id, lease.LeaseId); err != nil {
		return err
	}
	executionCtx, stopExecution := context.WithCancel(ctx)
	defer stopExecution()
	renewDone := make(chan struct{})
	renewErr := make(chan error, 1)
	go func() {
		defer close(renewDone)
		interval := time.Duration(lease.RenewAfterSeconds) * time.Second
		if interval <= 0 {
			interval = 10 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-executionCtx.Done():
				return
			case <-ticker.C:
				if _, err := a.client.RenewCommand(executionCtx, state, lease.Command.Id, lease.LeaseId); err != nil {
					select {
					case renewErr <- err:
					default:
					}
					stopExecution()
					return
				}
			}
		}
	}()
	progress := commandqueue.ProgressFunc(func(progressCtx context.Context, update contracts.CommandProgress) error {
		if update.CommandId == "" {
			update.CommandId = lease.Command.Id
		}
		if update.LeaseId == "" {
			update.LeaseId = lease.LeaseId
		}
		if update.ReportedAt == "" {
			update.ReportedAt = a.now().UTC().Format(time.RFC3339Nano)
		}
		if update.Status == "" {
			update.Status = commandqueue.StatusRunning
		}
		return a.client.ReportProgress(progressCtx, state, lease.Command.Id, update)
	})
	execution, err := a.executor.Execute(executionCtx, lease, progress)
	stopExecution()
	<-renewDone
	if err != nil {
		return err
	}
	select {
	case err := <-renewErr:
		if err != nil {
			return err
		}
	default:
	}
	if execution.Duplicate && execution.Result.Status == "" {
		return nil
	}
	if err := a.client.CompleteCommand(ctx, state, lease.Command.Id, execution.Result); err != nil {
		payload := struct {
			CommandID string                  `json:"command_id"`
			Result    contracts.CommandResult `json:"result"`
		}{lease.Command.Id, execution.Result}
		_, queueErr := a.outbox.Put(commandqueue.OutboxCommandResult, lease.Command.Id, payload)
		if queueErr != nil {
			return fmt.Errorf("upload command result: %v; persist outbox: %w", err, queueErr)
		}
		return err
	}
	return nil
}

func (a *Agent) drainOutbox(ctx context.Context) {
	state := a.getDeviceState()
	_, _ = a.outbox.Drain(ctx, a.config.OutboxBatchSize, func(uploadCtx context.Context, envelope commandqueue.Envelope) error {
		switch envelope.Kind {
		case commandqueue.OutboxHeartbeat:
			var heartbeat contracts.DeviceHeartbeat
			if err := json.Unmarshal(envelope.Payload, &heartbeat); err != nil {
				return err
			}
			return a.client.Heartbeat(uploadCtx, state, heartbeat)
		case commandqueue.OutboxCommandResult:
			var payload struct {
				CommandID string                  `json:"command_id"`
				Result    contracts.CommandResult `json:"result"`
			}
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				return err
			}
			return a.client.CompleteCommand(uploadCtx, state, payload.CommandID, payload.Result)
		default:
			return fmt.Errorf("outbox kind %q requires a contract endpoint not frozen in Nexus v1", envelope.Kind)
		}
	})
}

func (a *Agent) handleCommunicationError(err error) error {
	if errors.Is(err, ErrTokenRevoked) {
		_ = a.state.Revoke()
		state := a.getDeviceState()
		state.Revoked = true
		state.DeviceToken = ""
		a.setDeviceState(state)
		return ErrTokenRevoked
	}
	if errors.Is(err, ErrUnauthorized) {
		return ErrUnauthorized
	}
	return nil
}

func (a *Agent) getDeviceState() DeviceState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.deviceState
}

func (a *Agent) setDeviceState(state DeviceState) {
	a.mu.Lock()
	a.deviceState = state
	a.mu.Unlock()
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func jitter(duration time.Duration) time.Duration {
	if duration <= 0 {
		return 0
	}
	return time.Duration(float64(duration) * (0.8 + rand.Float64()*0.4))
}
