//go:build windows

package desktopruntime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var errTunnelManuallyStopped = errors.New("Tunnel 已主动停止")

type namedTunnelOptions struct {
	timings tunnelRecoveryTimings
	network func() (tunnelNetwork, error)
	command func(context.Context, tunnelRuntime, int, []string) (*exec.Cmd, error)
}

func defaultNamedTunnelOptions() namedTunnelOptions {
	return namedTunnelOptions{timings: defaultTunnelRecoveryTimings(), network: currentTunnelNetwork, command: namedTunnelCommand}
}

func namedTunnelCommand(ctx context.Context, previous tunnelRuntime, port int, resolvers []string) (*exec.Cmd, error) {
	// 恢复时重新读取 Token/component，避免长驻 supervisor 使用旧配置。
	runtime, err := loadTunnelRuntime(ctx, previous.root)
	if err != nil {
		return nil, err
	}
	if runtime.mode != "named" {
		return nil, fmt.Errorf("固定域名模式已改变")
	}
	if err := prepareCloudflaredRuntime(ctx, &runtime); err != nil {
		return nil, err
	}
	command, err := cloudflaredCommand(ctx, runtime)
	if err != nil {
		return nil, err
	}
	// cloudflaredCommand 的 Named 参数以 run 结尾。指标地址显式绑定本机，
	// 不依赖 cloudflared 默认端口搜索，也不覆盖用户显式选择的协议或解析器。
	arguments := append([]string{}, command.Args[:len(command.Args)-1]...)
	arguments = append(arguments, "--metrics", "127.0.0.1:"+strconv.Itoa(port))
	if strings.TrimSpace(os.Getenv("TUNNEL_TRANSPORT_PROTOCOL")) == "" {
		arguments = append(arguments, "--protocol", "auto")
	}
	arguments = append(arguments, "run")
	if strings.TrimSpace(os.Getenv("TUNNEL_DNS_RESOLVER_ADDRS")) == "" {
		for _, resolver := range resolvers {
			arguments = append(arguments, "--dns-resolver-addrs", resolver)
		}
	}
	command.Args = arguments
	return command, nil
}

func superviseNamedTunnel(parent context.Context, runtime tunnelRuntime, logs *processLogs, stopRequested func() (bool, error), options namedTunnelOptions) error {
	ctx, cancel := context.WithCancelCause(parent)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		interval := min(250*time.Millisecond, options.timings.ProbeInterval)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			stopped, err := stopRequested()
			if err != nil {
				cancel(err)
				return
			}
			if stopped {
				cancel(errTunnelManuallyStopped)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	defer func() { cancel(nil); <-watchDone; _ = os.Remove(filepath.Join(runtime.root, tunnelHealthFile)) }()
	client := newTunnelProbeClient()
	defer client.CloseIdleConnections()
	policy := newTunnelRecovery(options.timings)
	snapshot := tunnelHealthSnapshot{State: "connecting"}
	for ctx.Err() == nil {
		stopped, stopErr := stopRequested()
		if stopErr != nil {
			cancel(stopErr)
			break
		}
		if stopped {
			cancel(errTunnelManuallyStopped)
			break
		}
		network, err := options.network()
		if err != nil {
			network = tunnelNetwork{key: policy.networkKey}
		}
		snapshot.State = "connecting"
		snapshot.Reason = ""
		snapshot.NextRetryAt = time.Time{}
		err = runNamedTunnelChild(ctx, runtime, logs, network, options, policy, &snapshot, client)
		if ctx.Err() != nil {
			break
		}
		snapshot.Recoveries++
		snapshot.ProcessPID = 0
		snapshot.ReadyConnections = 0
		snapshot.OriginHealthy = false
		snapshot.PublicChecked = false
		snapshot.PublicReachable = false
		snapshot.State = "waiting_network"
		snapshot.Reason = "edge_disconnected"
		snapshot.NextRetryAt = policy.retryAt(time.Now())
		fmt.Fprintf(logs.stderr, "Named Tunnel 连接恢复：%v；下次尝试 %s，恢复次数 %d\n", err, snapshot.NextRetryAt.Format(time.RFC3339), snapshot.Recoveries)
		if err := waitNamedTunnelRetry(ctx, runtime.root, &snapshot, options.timings.ProbeInterval); err != nil {
			if ctx.Err() == nil {
				return err
			}
			break
		}
	}
	if errors.Is(context.Cause(ctx), errTunnelManuallyStopped) {
		return nil
	}
	return context.Cause(ctx)
}

func waitNamedTunnelRetry(ctx context.Context, root string, snapshot *tunnelHealthSnapshot, interval time.Duration) error {
	timer := time.NewTimer(time.Until(snapshot.NextRetryAt))
	defer timer.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := writeTunnelHealth(root, *snapshot); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case <-ticker.C:
		}
	}
}

func runNamedTunnelChild(ctx context.Context, runtime tunnelRuntime, logs *processLogs, network tunnelNetwork, options namedTunnelOptions, policy *tunnelRecovery, snapshot *tunnelHealthSnapshot, client *http.Client) error {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	command, err := options.command(ctx, runtime, port, network.resolvers)
	if err != nil {
		return err
	}
	command.Stdout = logs.stdout
	command.Stderr = logs.stderr
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	policy.started(time.Now())
	snapshot.ProcessPID = command.Process.Pid
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	reaped := false
	defer func() {
		if !reaped {
			_ = command.Process.Kill()
			<-done
		}
	}()
	if err := writeTunnelHealth(runtime.root, *snapshot); err != nil {
		return err
	}
	ticker := time.NewTicker(options.timings.ProbeInterval)
	defer ticker.Stop()
	lastState := ""
	probe := namedTunnelProbe{runtime: runtime, client: client, port: port, pid: command.Process.Pid, network: network, startedAt: policy.startedAt}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			reaped = true
			if err == nil {
				err = fmt.Errorf("cloudflared 意外退出")
			}
			return err
		case <-ticker.C:
		}
		ready := probe.check(ctx, options, snapshot, logs)
		restart := policy.observe(time.Now(), ready, probe.network.key)
		if err := writeTunnelHealth(runtime.root, *snapshot); err != nil {
			return err
		}
		if snapshot.State != lastState {
			fmt.Fprintf(logs.stderr, "Named Tunnel 状态：%s，活动连接 %d，恢复次数 %d\n", snapshot.State, snapshot.ReadyConnections, snapshot.Recoveries)
			lastState = snapshot.State
		}
		if restart {
			return fmt.Errorf("cloudflared 进程存活但持续没有可用连接")
		}
	}
}

type namedTunnelProbe struct {
	runtime         tunnelRuntime
	client          *http.Client
	port, pid       int
	network         tunnelNetwork
	startedAt       time.Time
	connectedOnce   bool
	lastPublicCheck time.Time
}

func (p *namedTunnelProbe) check(ctx context.Context, options namedTunnelOptions, snapshot *tunnelHealthSnapshot, logs *processLogs) bool {
	current, networkErr := options.network()
	if networkErr == nil && current.key != p.network.key {
		p.network = current
		snapshot.PublicChecked = false
		p.lastPublicCheck = time.Time{}
		fmt.Fprintln(logs.stderr, "Named Tunnel 检测到网卡、路由或 DNS 变化，正在检查连接")
	}
	connections := uint(0)
	if tunnelMetricsOwnedBy(p.port, p.pid) {
		connections, _ = probeTunnelReady(ctx, p.client, "http://127.0.0.1:"+strconv.Itoa(p.port))
	}
	snapshot.ReadyConnections = connections
	snapshot.OriginHealthy = probeAgentDockHealth(ctx, p.client, fmt.Sprintf("http://127.0.0.1:%d", p.runtime.settings.Port))
	snapshot.State = "reconnecting"
	snapshot.Reason = "edge_disconnected"
	if !p.connectedOnce && time.Since(p.startedAt) < options.timings.StartGrace {
		snapshot.State = "connecting"
	}
	if connections == 0 {
		snapshot.PublicChecked = false
		snapshot.PublicReachable = false
		p.lastPublicCheck = time.Time{}
		return false
	}
	p.connectedOnce = true
	snapshot.State = "connected"
	snapshot.Reason = ""
	if !snapshot.OriginHealthy {
		snapshot.State = "origin_unhealthy"
		snapshot.Reason = "origin_unhealthy"
		return true
	}
	if p.lastPublicCheck.IsZero() || time.Since(p.lastPublicCheck) >= 30*time.Second {
		publicURL, _ := readTunnelPublicURL(p.runtime)
		snapshot.PublicReachable = publicURL != "" && probeAgentDockHealth(ctx, p.client, strings.TrimRight(publicURL, "/"))
		snapshot.PublicChecked = true
		p.lastPublicCheck = time.Now()
	}
	if snapshot.PublicChecked && !snapshot.PublicReachable {
		snapshot.State = "public_unreachable"
		snapshot.Reason = "public_health_failed"
	}
	return true
}
