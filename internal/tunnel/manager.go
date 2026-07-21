package tunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/atomicfile"
)

// Mode selects how AgentDock is exposed remotely.
type Mode string

const (
	ModeDisabled   Mode = "disabled"
	ModeLoopback   Mode = "loopback"
	ModeLAN        Mode = "lan"
	ModeCloudflare Mode = "cloudflare"
	ModeCustom     Mode = "custom"
)

// Status is the operator-facing tunnel state.
type Status struct {
	Mode       Mode      `json:"mode"`
	State      string    `json:"state"` // disabled | starting | connected | error | stopped
	PublicURL  string    `json:"public_url,omitempty"`
	LocalURL   string    `json:"local_url,omitempty"`
	MCPURL     string    `json:"mcp_url,omitempty"`
	Provider   string    `json:"provider,omitempty"`
	PID        int       `json:"pid,omitempty"`
	Error      string    `json:"error,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
	Binary     string    `json:"binary,omitempty"`
	ConfigPath string    `json:"config_path,omitempty"`
}

// Config is persisted under ~/.agentdock/tunnel/config.json.
type Config struct {
	Mode           Mode   `json:"mode"`
	LocalHost      string `json:"local_host"`
	LocalPort      int    `json:"local_port"`
	CloudflaredBin string `json:"cloudflared_bin,omitempty"`
	// Named tunnel (optional). Empty => quick tunnel trycloudflare.com.
	TunnelName  string `json:"tunnel_name,omitempty"`
	TunnelToken string `json:"tunnel_token,omitempty"`
	CustomURL   string `json:"custom_url,omitempty"`
}

// Manager starts/stops reverse exposure for the MCP endpoint.
type Manager struct {
	home   string
	mu     sync.Mutex
	cmd    *exec.Cmd
	status Status
	cancel context.CancelFunc
}

func NewManager(agentDockHome string) (*Manager, error) {
	if strings.TrimSpace(agentDockHome) == "" {
		return nil, errors.New("agentdock home is required")
	}
	root := filepath.Join(agentDockHome, "tunnel")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	m := &Manager{home: agentDockHome}
	cfg, _ := m.LoadConfig()
	m.status = Status{
		Mode: cfg.Mode, State: "disabled", UpdatedAt: time.Now().UTC(),
		ConfigPath: m.configPath(),
	}
	if cfg.Mode == "" {
		m.status.Mode = ModeDisabled
	}
	return m, nil
}

func (m *Manager) configPath() string {
	return filepath.Join(m.home, "tunnel", "config.json")
}

func (m *Manager) LoadConfig() (Config, error) {
	data, err := os.ReadFile(m.configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return Config{Mode: ModeDisabled, LocalHost: "127.0.0.1", LocalPort: 8765}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.LocalHost == "" {
		cfg.LocalHost = "127.0.0.1"
	}
	if cfg.LocalPort == 0 {
		cfg.LocalPort = 8765
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeDisabled
	}
	return cfg, nil
}

func (m *Manager) SaveConfig(cfg Config) error {
	if cfg.LocalHost == "" {
		cfg.LocalHost = "127.0.0.1"
	}
	if cfg.LocalPort == 0 {
		cfg.LocalPort = 8765
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicfile.Write(m.configPath(), data, 0o600)
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status
	return s
}

// EnableCloudflareQuick starts cloudflared quick tunnel to local MCP.
func (m *Manager) EnableCloudflareQuick(ctx context.Context, localHost string, localPort int) (Status, error) {
	cfg, _ := m.LoadConfig()
	cfg.Mode = ModeCloudflare
	if localHost != "" {
		cfg.LocalHost = localHost
	}
	if localPort > 0 {
		cfg.LocalPort = localPort
	}
	if err := m.SaveConfig(cfg); err != nil {
		return Status{}, err
	}
	return m.Start(ctx)
}

// Start launches the configured tunnel mode.
func (m *Manager) Start(ctx context.Context) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil && m.cmd.Process != nil {
		return m.status, nil
	}
	cfg, err := m.LoadConfig()
	if err != nil {
		return Status{}, err
	}
	localURL := fmt.Sprintf("http://%s:%d", cfg.LocalHost, cfg.LocalPort)
	m.status = Status{
		Mode: cfg.Mode, State: "starting", LocalURL: localURL,
		UpdatedAt: time.Now().UTC(), ConfigPath: m.configPath(),
	}
	switch cfg.Mode {
	case ModeDisabled, "":
		m.status.State = "disabled"
		m.status.PublicURL = ""
		m.status.MCPURL = ""
		return m.status, nil
	case ModeLoopback:
		m.status.State = "connected"
		m.status.PublicURL = localURL
		m.status.MCPURL = strings.TrimRight(localURL, "/") + "/mcp"
		m.status.Provider = "loopback"
		return m.status, nil
	case ModeLAN:
		ip := firstNonLoopbackIPv4()
		if ip == "" {
			m.status.State = "error"
			m.status.Error = "no LAN IPv4 address found"
			return m.status, errors.New(m.status.Error)
		}
		url := fmt.Sprintf("http://%s:%d", ip, cfg.LocalPort)
		m.status.State = "connected"
		m.status.PublicURL = url
		m.status.MCPURL = url + "/mcp"
		m.status.Provider = "lan"
		return m.status, nil
	case ModeCustom:
		if strings.TrimSpace(cfg.CustomURL) == "" {
			m.status.State = "error"
			m.status.Error = "custom_url is required"
			return m.status, errors.New(m.status.Error)
		}
		m.status.State = "connected"
		m.status.PublicURL = strings.TrimRight(cfg.CustomURL, "/")
		m.status.MCPURL = m.status.PublicURL + "/mcp"
		m.status.Provider = "custom"
		return m.status, nil
	case ModeCloudflare:
		return m.startCloudflaredLocked(ctx, cfg, localURL)
	default:
		m.status.State = "error"
		m.status.Error = "unsupported tunnel mode"
		return m.status, errors.New(m.status.Error)
	}
}

func (m *Manager) startCloudflaredLocked(ctx context.Context, cfg Config, localURL string) (Status, error) {
	bin := cfg.CloudflaredBin
	if bin == "" {
		path, err := exec.LookPath("cloudflared")
		if err != nil {
			m.status.State = "error"
			m.status.Error = "cloudflared not found in PATH; install cloudflared or set cloudflared_bin"
			return m.status, errors.New(m.status.Error)
		}
		bin = path
	}
	m.status.Binary = bin
	m.status.Provider = "cloudflare"

	runCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	var cmd *exec.Cmd
	if strings.TrimSpace(cfg.TunnelToken) != "" {
		cmd = exec.CommandContext(runCtx, bin, "tunnel", "--no-autoupdate", "run", "--token", cfg.TunnelToken)
	} else {
		// Quick tunnel: public trycloudflare.com URL -> local service
		cmd = exec.CommandContext(runCtx, bin, "tunnel", "--no-autoupdate", "--url", localURL)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return Status{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return Status{}, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		m.status.State = "error"
		m.status.Error = err.Error()
		return m.status, err
	}
	m.cmd = cmd
	m.status.PID = cmd.Process.Pid
	m.status.State = "starting"

	urlCh := make(chan string, 1)
	// cloudflared prints the trycloudflare URL on stderr; scan both streams
	// concurrently. MultiReader would block on stdout forever and miss it.
	go scanCloudflaredOutput(stdout, urlCh)
	go scanCloudflaredOutput(stderr, urlCh)

	// Wait for quick tunnel URL (often 5–15s depending on network).
	wait := 45 * time.Second
	select {
	case <-ctx.Done():
		_ = m.stopLocked()
		return m.status, ctx.Err()
	case u := <-urlCh:
		m.status.PublicURL = strings.TrimRight(u, "/")
		m.status.MCPURL = m.status.PublicURL + "/mcp"
		m.status.State = "connected"
		m.status.Error = ""
		m.status.UpdatedAt = time.Now().UTC()
	case <-time.After(wait):
		// Named tunnel may not print trycloudflare URL; mark connected if process lives.
		if cfg.TunnelToken != "" || cfg.TunnelName != "" {
			m.status.State = "connected"
			m.status.Error = ""
			if cfg.CustomURL != "" {
				m.status.PublicURL = strings.TrimRight(cfg.CustomURL, "/")
				m.status.MCPURL = m.status.PublicURL + "/mcp"
			} else {
				m.status.Error = "named tunnel running; set custom_url to the public hostname"
			}
		} else {
			m.status.State = "error"
			m.status.Error = "timed out waiting for cloudflared public URL (check network / cloudflared logs)"
			_ = m.stopLocked()
			return m.status, errors.New(m.status.Error)
		}
	}
	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.cmd == cmd {
			m.cmd = nil
			if m.status.State == "connected" || m.status.State == "starting" {
				m.status.State = "stopped"
			}
			m.status.PID = 0
			m.status.UpdatedAt = time.Now().UTC()
		}
	}()
	return m.status, nil
}

func (m *Manager) Stop() (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.stopLocked(); err != nil {
		return m.status, err
	}
	m.status.State = "stopped"
	m.status.PID = 0
	m.status.UpdatedAt = time.Now().UTC()
	return m.status, nil
}

func (m *Manager) stopLocked() error {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		_, _ = m.cmd.Process.Wait()
		m.cmd = nil
	}
	return nil
}

// HealthCheck probes local MCP healthz if LocalURL is set.
func (m *Manager) HealthCheck(ctx context.Context) error {
	st := m.Status()
	base := st.LocalURL
	if base == "" {
		cfg, err := m.LoadConfig()
		if err != nil {
			return err
		}
		base = fmt.Sprintf("http://%s:%d", cfg.LocalHost, cfg.LocalPort)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("healthz status %d", resp.StatusCode)
	}
	return nil
}

var tryCloudflareURL = regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.trycloudflare\.com`)

func scanCloudflaredOutput(r io.Reader, urlCh chan<- string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if u := tryCloudflareURL.FindString(line); u != "" {
			select {
			case urlCh <- u:
			default:
			}
			return
		}
	}
}

func firstNonLoopbackIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue
			}
			return ip.String()
		}
	}
	return ""
}
