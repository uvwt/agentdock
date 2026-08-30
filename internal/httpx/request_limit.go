package httpx

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/config"
)

type fixedWindowLimiter struct {
	mu      sync.Mutex
	maximum int
	window  time.Duration
	entries map[string]fixedWindowEntry
}

type fixedWindowEntry struct {
	started time.Time
	count   int
}

func newFixedWindowLimiter(maximum int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{maximum: maximum, window: window, entries: map[string]fixedWindowEntry{}}
}

func (l *fixedWindowLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, exists := l.entries[key]
	if !exists && len(l.entries) >= 4096 {
		for candidate, value := range l.entries {
			if now.Sub(value.started) >= l.window {
				delete(l.entries, candidate)
			}
		}
		if len(l.entries) >= 4096 {
			return false
		}
	}
	if entry.started.IsZero() || now.Sub(entry.started) >= l.window {
		l.entries[key] = fixedWindowEntry{started: now, count: 1}
		return true
	}
	if entry.count >= l.maximum {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}

func requestRemoteIP(r *http.Request, cfg config.Config) string {
	remote := parseRemoteIP(r.RemoteAddr)
	if remote == nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	networks := trustedProxyNetworks(cfg.TrustedProxyCIDRs)
	if !ipInNetworks(remote, networks) {
		return remote.String()
	}

	rawForwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if rawForwarded == "" {
		return remote.String()
	}
	parts := strings.Split(rawForwarded, ",")
	chain := make([]net.IP, 0, len(parts)+1)
	for _, part := range parts {
		ip := net.ParseIP(strings.TrimSpace(part))
		if ip == nil {
			// 可信代理传来的链必须完全可解析；异常链回退到直接对端，不能部分信任。
			return remote.String()
		}
		chain = append(chain, ip)
	}
	chain = append(chain, remote)
	for index := len(chain) - 1; index >= 0; index-- {
		if !ipInNetworks(chain[index], networks) {
			return chain[index].String()
		}
	}
	return chain[0].String()
}
func parseRemoteIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err == nil {
		return net.ParseIP(host)
	}
	return net.ParseIP(strings.Trim(strings.TrimSpace(remoteAddr), "[]"))
}
func trustedProxyNetworks(values []string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err == nil {
			networks = append(networks, network)
		}
	}
	return networks
}
func ipInNetworks(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
