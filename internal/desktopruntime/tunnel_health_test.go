package desktopruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTunnelReadyRequiresCurrentConnection(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       uint
	}{
		{"connected", `{"readyConnections":2,"connectorId":"test"}`, 200, 2},
		{"disconnected", `{"readyConnections":0}`, 503, 0},
		{"unrelated server", `{"ok":true}`, 200, 0},
		{"stale positive body", `{"readyConnections":2}`, 503, 0},
		{"invalid response", `{`, 200, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			got, _ := probeTunnelReady(context.Background(), server.Client(), server.URL)
			if got != tc.want {
				t.Fatalf("connections=%d, want %d", got, tc.want)
			}
		})
	}
}

func TestTunnelHealthRejectsRedirectAndHonorsCancellation(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("probe followed redirect")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer server.Close()
	client := newTunnelProbeClient()
	defer client.CloseIdleConnections()
	if probeAgentDockHealth(context.Background(), client, server.URL) {
		t.Fatal("redirect accepted as health")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if probeAgentDockHealth(ctx, client, target.URL) {
		t.Fatal("canceled probe accepted")
	}
}

func TestTunnelRecoveryWaitsForSustainedDisconnection(t *testing.T) {
	now := time.Now()
	p := newTunnelRecovery(defaultTunnelRecoveryTimings())
	p.started(now)
	if p.observe(now.Add(10*time.Second), false, "vpn-a") {
		t.Fatal("restarted during startup grace")
	}
	if !p.observe(now.Add(46*time.Second), false, "vpn-a") {
		t.Fatal("live disconnected process was not recovered")
	}
	p.started(now.Add(time.Minute))
	if p.observe(now.Add(2*time.Minute), true, "vpn-a") {
		t.Fatal("healthy process restarted")
	}
	if p.observe(now.Add(2*time.Minute+time.Second), false, "vpn-b") {
		t.Fatal("brief VPN transition caused restart")
	}
	if !p.observe(now.Add(3*time.Minute), false, "vpn-b") {
		t.Fatal("VPN disconnection was not recovered")
	}
}

func TestTunnelNetworkChangeDoesNotRestartHealthyProcess(t *testing.T) {
	now := time.Now()
	p := newTunnelRecovery(defaultTunnelRecoveryTimings())
	p.started(now)
	for i := 1; i < 20; i++ {
		if p.observe(now.Add(time.Duration(i)*time.Minute), true, string(rune('a'+i))) {
			t.Fatal("network change restarted healthy tunnel")
		}
	}
}

func TestTunnelRecoveryRateLimitAndHealthyReset(t *testing.T) {
	now := time.Now()
	p := newTunnelRecovery(defaultTunnelRecoveryTimings())
	for i := 0; i < 3; i++ {
		p.started(now.Add(time.Duration(i) * time.Second))
	}
	if got := p.retryAt(now.Add(10 * time.Second)); got.Before(now.Add(5 * time.Minute)) {
		t.Fatalf("restart storm not limited: %v", got)
	}
	p.observe(now.Add(time.Minute), true, "vpn")
	p.observe(now.Add(4*time.Minute), true, "vpn")
	if got := p.retryAt(now.Add(4 * time.Minute)); got.After(now.Add(4*time.Minute + 10*time.Second)) {
		t.Fatalf("healthy run did not reset retry budget: %v", got)
	}
}

func TestTunnelHealthSnapshotRejectsStaleOrDifferentSupervisor(t *testing.T) {
	now := time.Now()
	s := tunnelHealthSnapshot{SupervisorPID: 123, ObservedAt: now, State: "connected", ReadyConnections: 2, OriginHealthy: true}
	if !s.fresh(now, 123) {
		t.Fatal("current snapshot rejected")
	}
	if s.fresh(now, 124) || s.fresh(now.Add(time.Minute), 123) {
		t.Fatal("stale supervisor snapshot accepted")
	}
}

func TestTunnelStatusRequiresLiveOwnerChildAndFreshHealth(t *testing.T) {
	now := time.Now()
	snapshot := tunnelHealthSnapshot{SupervisorPID: 123, ObservedAt: now, State: "connected", ReadyConnections: 2, OriginHealthy: true}
	for _, tc := range []struct {
		name  string
		owner int
		child bool
		age   time.Duration
		want  bool
		state string
	}{
		{"current", 123, true, 0, true, "connected"},
		{"old supervisor", 124, true, 0, false, "unknown"},
		{"expired", 123, true, time.Minute, false, "unknown"},
		{"exited child", 123, false, 0, false, "reconnecting"},
		{"manual stop", 0, false, 0, false, "stopped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := TunnelStatus{Running: tc.child}
			applyNamedTunnelHealth(&status, snapshot, now.Add(tc.age), tc.owner, tc.child)
			if status.Ready != tc.want || status.ConnectionState != tc.state {
				t.Fatalf("status=%+v", status)
			}
		})
	}
}
