package resourcelibrary

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/nexusclient"
)

// packageTransferIP is only for an authenticated HTTPS transfer to the paired
// Nexus origin. In addition to public addresses, permit RFC 2544 benchmarking
// addresses commonly used as proxy-TUN fake IPs. Keep literal-host validation
// strict (publicIP), and preserve TLS hostname verification and origin pinning.
func packageTransferIP(ip net.IP) bool {
	if publicIP(ip) {
		return true
	}
	v4 := ip.To4()
	return v4 != nil && v4[0] == 198 && (v4[1] == 18 || v4[1] == 19)
}

// packageClient 是资源包下载和上传专用客户端。
// 它不使用普通 nexusclient.New 的默认拨号：默认拨号会在授权解析之后按域名再查一次 DNS。
func packageClient(endpoint, token string) (nexusclient.Client, error) {
	if strings.TrimSpace(token) == "" {
		return nexusclient.Client{}, failed("NEXUS_NOT_PAIRED", "validation", "Nexus device token is unavailable")
	}
	transport, err := packageTransport(endpoint, systemLookup, packageTransferIP)
	if err != nil {
		return nexusclient.Client{}, err
	}
	return nexusclient.NewWithTransport(endpoint, token, transport), nil
}

func packageTransport(endpoint string, lookup ipLookup, allow func(net.IP) bool) (*http.Transport, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, failed("DOWNLOAD_URL_REJECTED", "validation", "paired Nexus endpoint is not an HTTPS origin")
	}
	host := strings.TrimSuffix(parsed.Hostname(), ".")
	if rejectedHost(host) || allow == nil || lookup == nil {
		return nil, failed("DOWNLOAD_URL_REJECTED", "validation", "paired Nexus endpoint is not a public HTTPS origin")
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok || base == nil {
		return nil, failed("DOWNLOAD_FAILED", "validation", "package transport is unavailable")
	}
	transport := base.Clone()
	// 明确不用 HTTP_PROXY、HTTPS_PROXY 和 ALL_PROXY。环境代理可以把连接转到调用方没校验过的私网地址。
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = pinnedDialer{
		host:   host,
		port:   canonicalPort(parsed),
		allow:  allow,
		lookup: lookup,
		dial:   dialer.DialContext,
	}.DialContext
	// TLSClientConfig 保持默认。SNI 和证书主机名来自请求 URL 的配对域名，而不是拨号用的 IP。
	return transport, nil
}

type ipLookup func(context.Context, string) ([]net.IP, error)

type tcpDial func(context.Context, string, string) (net.Conn, error)

type pinnedDialer struct {
	host   string
	port   string
	allow  func(net.IP) bool
	lookup ipLookup
	dial   tcpDial
}

func (d pinnedDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, errPackageDial
	}
	host = strings.Trim(strings.TrimSuffix(host, "."), "[]")
	// 只允许拨配对原点。重定向或其他 Host 在这里失败，Device Token 不会被送到那个连接。
	if !strings.EqualFold(host, d.host) || port != d.port {
		return nil, errPackageDial
	}
	ips, err := d.resolve(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, errPackageDial
	}
	selected := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if d.allow == nil || !d.allow(ip) {
			// 任一结果不是公网就整次失败，不能从混合结果里挑一个继续连。
			return nil, errPackageDial
		}
		if !networkAllows(network, ip) {
			continue
		}
		selected = append(selected, ip)
	}
	if len(selected) == 0 {
		return nil, errPackageDial
	}
	var dialErr error
	for _, ip := range selected {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// 连刚才判定过的 IP，不再把域名交给系统拨号器。
		conn, err := d.dial(ctx, "tcp", net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		dialErr = err
	}
	if dialErr == nil {
		dialErr = errPackageDial
	}
	return nil, dialErr
}

func (d pinnedDialer) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	if d.lookup == nil {
		return nil, errPackageDial
	}
	return d.lookup(ctx, host)
}

func systemLookup(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		if addr.IP != nil {
			ips = append(ips, addr.IP)
		}
	}
	if len(ips) == 0 {
		return nil, errPackageDial
	}
	return ips, nil
}

func networkAllows(network string, ip net.IP) bool {
	switch network {
	case "tcp4":
		return ip.To4() != nil
	case "tcp6":
		return ip.To4() == nil && ip.To16() != nil
	default:
		return true
	}
}

var errPackageDial = errors.New("package connection is not pinned to a public address")
