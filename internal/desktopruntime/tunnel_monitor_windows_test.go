//go:build windows

package desktopruntime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNamedTunnelCommandRefreshesResolversAndHonorsOverrides(t *testing.T) {
	root := t.TempDir()
	manifest := Manifest{SchemaVersion: SchemaVersion, AgentDockBinary: filepath.Join(root, "agentdock.exe"), Host: "127.0.0.1", Port: 28765, LocalMCPURL: "http://127.0.0.1:28765/mcp", TunnelMode: "named", PublicURL: "https://tunnel.example.invalid"}
	if err := Save(filepath.Join(root, "runtime.json"), manifest); err != nil {
		t.Fatal(err)
	}
	componentRoot := filepath.Join(root, "components", "cloudflared")
	binary := filepath.Join(componentRoot, "versions", "2026.9.1", "cloudflared.exe")
	if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("test binary; command is inspected without execution")
	if err := os.WriteFile(binary, content, 0600); err != nil {
		t.Fatal(err)
	}
	pointer := fmt.Sprintf(`{"schema_version":1,"component":"cloudflared","version":"2026.9.1","sha256":"%x"}`, sha256.Sum256(content))
	if err := os.WriteFile(filepath.Join(componentRoot, "active.json"), []byte(pointer), 0600); err != nil {
		t.Fatal(err)
	}
	const token = "isolated-test-token"
	if err := writeProtectedText(filepath.Join(root, "cloudflared-token.dpapi"), token, tunnelTokenEntropy); err != nil {
		t.Fatal(err)
	}
	for _, override := range []bool{false, true} {
		t.Run(strconv.FormatBool(override), func(t *testing.T) {
			protocol, dns := "", ""
			if override {
				protocol, dns = "http2", "127.0.0.9:53"
			}
			t.Setenv("TUNNEL_TRANSPORT_PROTOCOL", protocol)
			t.Setenv("TUNNEL_DNS_RESOLVER_ADDRS", dns)
			command, err := namedTunnelCommand(context.Background(), tunnelRuntime{root: root}, 12345, []string{"127.0.0.2:53", "[::1]:53"})
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Join(command.Args, " ")
			if !strings.Contains(args, "--metrics 127.0.0.1:12345") || strings.Contains(args, token) {
				t.Fatal("metrics binding or token isolation incorrect")
			}
			if override {
				if strings.Contains(args, "--protocol") || strings.Contains(args, "--dns-resolver-addrs") {
					t.Fatal("explicit environment overrides were replaced")
				}
			} else if !strings.Contains(args, "--protocol auto run --dns-resolver-addrs 127.0.0.2:53 --dns-resolver-addrs [::1]:53") {
				t.Fatalf("incorrect protocol or fresh resolver arguments: %s", args)
			}
			if !strings.Contains(strings.Join(command.Env, "\n"), "TUNNEL_TOKEN="+token) {
				t.Fatal("token not passed through child environment")
			}
		})
	}
}

func TestNamedTunnelRecoversLiveDisconnectedProcessAndHonorsStop(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) }))
	defer origin.Close()
	_, portText, _ := net.SplitHostPort(origin.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	root := t.TempDir()
	runtime := tunnelRuntime{root: root, mode: "named", settings: controlPanelSettings{Port: port}, files: tunnelFiles{serverURL: filepath.Join(root, "server-url.txt")}}
	if err := os.WriteFile(runtime.files.serverURL, []byte("https://127.0.0.1:1"), 0600); err != nil {
		t.Fatal(err)
	}
	logs, err := openProcessLogs(filepath.Join(root, "out.log"), filepath.Join(root, "err.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	var starts atomic.Int32
	var network atomic.Int32
	var stopped atomic.Bool
	resolved := make(chan []string, 4)
	options := defaultNamedTunnelOptions()
	options.timings.ProbeInterval = 30 * time.Millisecond
	options.timings.StartGrace = 120 * time.Millisecond
	options.timings.DisconnectGrace = 90 * time.Millisecond
	options.timings.NetworkSettle = 30 * time.Millisecond
	options.network = func() (tunnelNetwork, error) {
		n := network.Load()
		return tunnelNetwork{key: fmt.Sprint(n), resolvers: []string{fmt.Sprintf("127.0.0.%d:53", n+1)}}, nil
	}
	options.command = func(ctx context.Context, _ tunnelRuntime, port int, dns []string) (*exec.Cmd, error) {
		n := starts.Add(1)
		resolved <- append([]string{}, dns...)
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNamedTunnelMonitorHelperProcess$")
		command.Env = append(os.Environ(), "AGENTDOCK_TEST_TUNNEL_MONITOR=1", "AGENTDOCK_TEST_TUNNEL_PORT="+strconv.Itoa(port), "AGENTDOCK_TEST_TUNNEL_CONNECTED="+strconv.FormatBool(n > 1))
		return command, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- superviseNamedTunnel(ctx, runtime, logs, func() (bool, error) { return stopped.Load(), nil }, options)
	}()
	first := waitTunnelMonitorSnapshot(t, ctx, root, func(s tunnelHealthSnapshot) bool { return s.ProcessPID > 0 })
	network.Store(1)
	waitTunnelMonitorSnapshot(t, ctx, root, func(s tunnelHealthSnapshot) bool { return s.State == "connected" || s.State == "public_unreachable" })
	if starts.Load() != 2 {
		t.Fatalf("child starts=%d, want one replacement", starts.Load())
	}
	if _, err := queryProcessPath(uint32(first.ProcessPID)); err == nil {
		t.Fatal("replacement started before old process exited")
	}
	<-resolved
	if dns := <-resolved; len(dns) != 1 || dns[0] != "127.0.0.2:53" {
		t.Fatalf("replacement retained old DNS: %v", dns)
	}
	network.Store(2)
	time.Sleep(250 * time.Millisecond)
	if starts.Load() != 2 {
		t.Fatal("healthy process restarted after network change or public health failure")
	}
	stopped.Store(true)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("stop did not cancel monitor")
	}
	if _, err := os.Stat(filepath.Join(root, tunnelHealthFile)); !os.IsNotExist(err) {
		t.Fatalf("health snapshot retained after stop: %v", err)
	}
}

func TestNamedTunnelStopCancelsRetryWait(t *testing.T) {
	root := t.TempDir()
	runtime := tunnelRuntime{root: root, mode: "named"}
	logs, err := openProcessLogs(filepath.Join(root, "out.log"), filepath.Join(root, "err.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	var stopped atomic.Bool
	var starts atomic.Int32
	options := defaultNamedTunnelOptions()
	options.timings.ProbeInterval = 20 * time.Millisecond
	options.network = func() (tunnelNetwork, error) { return tunnelNetwork{key: "vpn"}, nil }
	options.command = func(ctx context.Context, _ tunnelRuntime, _ int, _ []string) (*exec.Cmd, error) {
		starts.Add(1)
		return exec.CommandContext(ctx, "cmd.exe", "/c", "exit", "1"), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- superviseNamedTunnel(ctx, runtime, logs, func() (bool, error) { return stopped.Load(), nil }, options)
	}()
	waitTunnelMonitorSnapshot(t, ctx, root, func(s tunnelHealthSnapshot) bool { return s.State == "waiting_network" })
	stopped.Store(true)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("retry wait ignored manual stop")
	}
	if starts.Load() != 1 {
		t.Fatal("manual stop launched a replacement")
	}
}

func TestTunnelMetricsPortRejectsDifferentOwner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"readyConnections":2,"connectorId":"other"}`))
	}))
	defer server.Close()
	_, portText, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	if !tunnelMetricsOwnedBy(port, os.Getpid()) {
		t.Fatal("own listener not identified")
	}
	if tunnelMetricsOwnedBy(port, os.Getpid()+100000) {
		t.Fatal("another process listener accepted")
	}
}

func TestNamedTunnelOriginFailureKeepsConnectedChild(t *testing.T) {
	metrics := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"readyConnections":2,"connectorId":"current"}`))
	}))
	defer metrics.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false}`))
	}))
	defer origin.Close()
	_, metricsPort, _ := net.SplitHostPort(metrics.Listener.Addr().String())
	_, originPort, _ := net.SplitHostPort(origin.Listener.Addr().String())
	port, _ := strconv.Atoi(metricsPort)
	corePort, _ := strconv.Atoi(originPort)
	root := t.TempDir()
	logs, err := openProcessLogs(filepath.Join(root, "out.log"), filepath.Join(root, "err.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	options := defaultNamedTunnelOptions()
	options.network = func() (tunnelNetwork, error) { return tunnelNetwork{key: "vpn"}, nil }
	probe := namedTunnelProbe{runtime: tunnelRuntime{settings: controlPanelSettings{Port: corePort}}, client: newTunnelProbeClient(), port: port, pid: os.Getpid(), network: tunnelNetwork{key: "vpn"}}
	defer probe.client.CloseIdleConnections()
	var snapshot tunnelHealthSnapshot
	if !probe.check(context.Background(), options, &snapshot, logs) || snapshot.State != "origin_unhealthy" || snapshot.ReadyConnections != 2 {
		t.Fatalf("origin failure treated as edge disconnection: %+v", snapshot)
	}
}

func TestNamedTunnelMonitorHelperProcess(t *testing.T) {
	if os.Getenv("AGENTDOCK_TEST_TUNNEL_MONITOR") != "1" {
		t.Skip("helper process only")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:"+os.Getenv("AGENTDOCK_TEST_TUNNEL_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	err = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ready" {
			http.NotFound(w, r)
			return
		}
		if os.Getenv("AGENTDOCK_TEST_TUNNEL_CONNECTED") != "true" {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"readyConnections":0}`))
			return
		}
		_, _ = w.Write([]byte(`{"readyConnections":2,"connectorId":"helper"}`))
	}))
	t.Fatal(err)
}

func waitTunnelMonitorSnapshot(t *testing.T, ctx context.Context, root string, accept func(tunnelHealthSnapshot) bool) tunnelHealthSnapshot {
	t.Helper()
	for ctx.Err() == nil {
		if s, err := readTunnelHealth(root); err == nil && accept(s) {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	s, _ := readTunnelHealth(root)
	t.Fatalf("monitor did not reach expected state: %+v", s)
	return s
}

func TestNamedTunnelManualStopBeforeLaunch(t *testing.T) {
	root := t.TempDir()
	logs, err := openProcessLogs(filepath.Join(root, "out.log"), filepath.Join(root, "err.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	options := defaultNamedTunnelOptions()
	options.network = func() (tunnelNetwork, error) { return tunnelNetwork{}, nil }
	var starts atomic.Int32
	options.command = func(ctx context.Context, _ tunnelRuntime, _ int, _ []string) (*exec.Cmd, error) {
		starts.Add(1)
		return exec.CommandContext(ctx, "cmd.exe", "/c", "exit", "1"), nil
	}
	if err := superviseNamedTunnel(context.Background(), tunnelRuntime{root: root}, logs, func() (bool, error) { return true, nil }, options); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 0 {
		t.Fatal("stopped supervisor attempted to create child")
	}
}

func TestTunnelNetworkSnapshotStableWithoutChanges(t *testing.T) {
	first, err := currentTunnelNetwork()
	if err != nil {
		t.Fatal(err)
	}
	second, err := currentTunnelNetwork()
	if err != nil {
		t.Fatal(err)
	}
	if first.key == "" || first.key != second.key {
		t.Fatalf("network snapshot unstable without configuration changes: %s / %s", first.key, second.key)
	}
}
