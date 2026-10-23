package desktopruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
)

const tunnelHealthFile = "tunnel-health.json"

// 该快照只由拥有 Tunnel mutex 的 supervisor 写入，CLI/UI 不另行推断连接状态。
type tunnelHealthSnapshot struct {
	SupervisorPID    int       `json:"supervisor_pid"`
	ProcessPID       int       `json:"process_pid"`
	ObservedAt       time.Time `json:"observed_at"`
	State            string    `json:"state"`
	ReadyConnections uint      `json:"ready_connections"`
	OriginHealthy    bool      `json:"origin_healthy"`
	PublicChecked    bool      `json:"public_checked"`
	PublicReachable  bool      `json:"public_reachable"`
	Recoveries       int       `json:"recoveries"`
	Reason           string    `json:"reason,omitempty"`
	NextRetryAt      time.Time `json:"next_retry_at,omitempty"`
}

func (s tunnelHealthSnapshot) fresh(now time.Time, supervisorPID int) bool {
	age := now.Sub(s.ObservedAt)
	return supervisorPID > 0 && s.SupervisorPID == supervisorPID && age >= -5*time.Second && age <= 20*time.Second
}

func applyNamedTunnelHealth(status *TunnelStatus, snapshot tunnelHealthSnapshot, now time.Time, owner int, childMatches bool) {
	status.Ready = false
	status.ConnectionState = "stopped"
	if owner == 0 {
		return
	}
	status.ConnectionState = "unknown"
	if !snapshot.fresh(now, owner) {
		return
	}
	status.ConnectionState = snapshot.State
	status.ReadyConnections = snapshot.ReadyConnections
	status.OriginHealthy = snapshot.OriginHealthy
	status.PublicChecked = snapshot.PublicChecked
	status.PublicReachable = snapshot.PublicReachable
	status.RecoveryCount = snapshot.Recoveries
	status.RecoveryReason = snapshot.Reason
	if snapshot.ReadyConnections > 0 && (!status.Running || !childMatches) {
		status.ConnectionState = "reconnecting"
		status.ReadyConnections = 0
		status.PublicChecked = false
		status.PublicReachable = false
		status.RecoveryReason = "process_exited"
	}
	status.Ready = status.Running && childMatches && status.ReadyConnections > 0 && snapshot.OriginHealthy
}

func writeTunnelHealth(root string, s tunnelHealthSnapshot) error {
	s.SupervisorPID = os.Getpid()
	s.ObservedAt = time.Now().UTC()
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(root, tunnelHealthFile), data, 0o600)
}

func readTunnelHealth(root string) (tunnelHealthSnapshot, error) {
	var s tunnelHealthSnapshot
	data, err := os.ReadFile(filepath.Join(root, tunnelHealthFile))
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(data, &s)
	return s, err
}

func newTunnelProbeClient() *http.Client {
	return &http.Client{
		Timeout:       3 * time.Second,
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func tunnelProbeJSON(ctx context.Context, client *http.Client, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(target)
}

func probeTunnelReady(ctx context.Context, client *http.Client, origin string) (uint, error) {
	var body struct {
		ReadyConnections uint   `json:"readyConnections"`
		ConnectorID      string `json:"connectorId"`
	}
	if err := tunnelProbeJSON(ctx, client, origin+"/ready", &body); err != nil {
		return 0, err
	}
	if body.ReadyConnections == 0 || body.ConnectorID == "" {
		return 0, fmt.Errorf("tunnel has no ready connection")
	}
	return body.ReadyConnections, nil
}

func probeAgentDockHealth(ctx context.Context, client *http.Client, origin string) bool {
	var body struct {
		OK bool `json:"ok"`
	}
	return tunnelProbeJSON(ctx, client, origin+"/healthz", &body) == nil && body.OK
}

type tunnelRecoveryTimings struct {
	ProbeInterval, StartGrace, DisconnectGrace, NetworkSettle time.Duration
	RetryWindow, StablePeriod                                 time.Duration
	MaxAttempts                                               int
}

func defaultTunnelRecoveryTimings() tunnelRecoveryTimings {
	return tunnelRecoveryTimings{
		ProbeInterval: 5 * time.Second, StartGrace: 45 * time.Second, DisconnectGrace: 30 * time.Second,
		NetworkSettle: 3 * time.Second, RetryWindow: 5 * time.Minute, StablePeriod: 2 * time.Minute, MaxAttempts: 3,
	}
}

type tunnelRecovery struct {
	timings                                                tunnelRecoveryTimings
	startedAt, failedSince, healthySince, networkChangedAt time.Time
	networkKey                                             string
	attempts                                               []time.Time
	retryDelay                                             time.Duration
}

func newTunnelRecovery(timings tunnelRecoveryTimings) *tunnelRecovery {
	return &tunnelRecovery{timings: timings}
}

func (p *tunnelRecovery) started(now time.Time) {
	p.startedAt, p.failedSince, p.healthySince = now, time.Time{}, time.Time{}
	p.attempts = append(p.attempts, now)
}

func (p *tunnelRecovery) observe(now time.Time, ready bool, networkKey string) bool {
	if networkKey != "" && networkKey != p.networkKey {
		if p.networkKey != "" {
			p.networkChangedAt = now
		}
		p.networkKey = networkKey
	}
	if ready {
		p.failedSince = time.Time{}
		if p.healthySince.IsZero() {
			p.healthySince = now
		}
		if now.Sub(p.healthySince) >= p.timings.StablePeriod {
			p.attempts = nil
			p.retryDelay = 0
		}
		return false
	}
	p.healthySince = time.Time{}
	if p.failedSince.IsZero() {
		p.failedSince = now
	}
	return now.Sub(p.startedAt) >= p.timings.StartGrace &&
		now.Sub(p.failedSince) >= p.timings.DisconnectGrace &&
		(p.networkChangedAt.IsZero() || now.Sub(p.networkChangedAt) >= p.timings.NetworkSettle)
}

func (p *tunnelRecovery) retryAt(now time.Time) time.Time {
	p.retryDelay = nextTunnelRetryDelay(p.retryDelay, 0)
	// 抖动只增加等待，避免低于限流窗口。
	next := now.Add(p.retryDelay + time.Duration(rand.Int64N(int64(p.retryDelay/5)+1)))
	cutoff := now.Add(-p.timings.RetryWindow)
	for len(p.attempts) > 0 && !p.attempts[0].After(cutoff) {
		p.attempts = p.attempts[1:]
	}
	if len(p.attempts) >= p.timings.MaxAttempts {
		limit := p.attempts[len(p.attempts)-p.timings.MaxAttempts].Add(p.timings.RetryWindow)
		if limit.After(next) {
			next = limit
		}
	}
	return next
}
