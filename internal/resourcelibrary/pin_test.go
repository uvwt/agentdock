package resourcelibrary

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/uvwt/agentdock/internal/nexusclient"
)

func TestPinnedDial_重复解析到私网时不再拨号(t *testing.T) {
	var lookups int
	var dialed []string
	dialer := pinnedDialer{
		host:  "library.example",
		port:  "443",
		allow: publicIP,
		lookup: func(_ context.Context, host string) ([]net.IP, error) {
			if host != "library.example" {
				t.Fatalf("lookup host = %s", host)
			}
			lookups++
			if lookups == 1 {
				return []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("2606:4700:4700::1111")}, nil
			}
			return []net.IP{net.ParseIP("10.1.2.3"), net.ParseIP("1.1.1.1")}, nil
		},
		dial: func(_ context.Context, _, address string) (net.Conn, error) {
			dialed = append(dialed, address)
			return nil, errors.New("recorded")
		},
	}
	if _, err := dialer.DialContext(context.Background(), "tcp", "library.example:443"); err == nil {
		t.Fatal("public dial returned a connection")
	}
	if len(dialed) != 2 || dialed[0] != "1.1.1.1:443" || dialed[1] != "[2606:4700:4700::1111]:443" {
		t.Fatalf("dialed = %#v", dialed)
	}
	if _, err := dialer.DialContext(context.Background(), "tcp", "library.example:443"); !errors.Is(err, errPackageDial) {
		t.Fatalf("rebind error = %v", err)
	}
	if len(dialed) != 2 || lookups != 2 {
		t.Fatalf("after rebind dialed = %#v lookups = %d", dialed, lookups)
	}
	if _, err := dialer.DialContext(context.Background(), "tcp", "evil.example:443"); !errors.Is(err, errPackageDial) || lookups != 2 {
		t.Fatalf("other host error = %v lookups = %d", err, lookups)
	}
}

func TestPinnedDial_混合私网结果直接失败(t *testing.T) {
	var dialed int
	dialer := pinnedDialer{
		host:  "library.example",
		port:  "443",
		allow: publicIP,
		lookup: func(context.Context, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("10.0.0.8")}, nil
		},
		dial: func(context.Context, string, string) (net.Conn, error) {
			dialed++
			return nil, errors.New("dialed")
		},
	}
	cases := [][]net.IP{
		{net.ParseIP("1.1.1.1"), net.ParseIP("10.0.0.8")},
		{net.ParseIP("127.0.0.1")},
		{net.ParseIP("169.254.169.254")},
		{net.ParseIP("::1")},
		{net.ParseIP("fd00:ec2::254")},
		{net.ParseIP("2001:db8::1")},
		{net.ParseIP("::ffff:127.0.0.1")},
		{net.ParseIP("2002:0a00:0001::")},
		{net.ParseIP("64:ff9b::7f00:1")},
	}
	for _, ips := range cases {
		dialer.lookup = func(context.Context, string) ([]net.IP, error) { return ips, nil }
		if _, err := dialer.DialContext(context.Background(), "tcp", "library.example:443"); !errors.Is(err, errPackageDial) {
			t.Fatalf("ips %v error = %v", ips, err)
		}
	}
	if dialed != 0 {
		t.Fatalf("private results dialed %d times", dialed)
	}
}

func TestPackageTransport_拒绝私网且不使用环境代理(t *testing.T) {
	var proxyCalls atomic.Int32
	var originCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Errorf("proxy saw Authorization")
		}
		http.Error(w, "proxy", http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("https_proxy", proxy.URL)
	t.Setenv("ALL_PROXY", proxy.URL)
	t.Setenv("all_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	blocked, err := packageTransport("https://example.com", func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}, publicIP)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Proxy != nil {
		t.Fatal("package transport uses a proxy function")
	}
	if _, err := blocked.DialContext(context.Background(), "tcp", "example.com:443"); !errors.Is(err, errPackageDial) {
		t.Fatalf("loopback dial = %v", err)
	}
	if _, err := blocked.DialContext(context.Background(), "tcp", "169.254.169.254:443"); !errors.Is(err, errPackageDial) {
		t.Fatalf("metadata host dial = %v", err)
	}

	var seenHost, seenSNI, seenAuth string
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originCalls.Add(1)
		seenHost = r.Host
		seenAuth = r.Header.Get("Authorization")
		if r.TLS != nil {
			seenSNI = r.TLS.ServerName
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer origin.Close()
	endpoint, client := pinnedTestClient(t, origin, "device-token")
	response, err := client.Do(context.Background(), http.MethodGet, "/pkg.zip", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent || originCalls.Load() != 1 || proxyCalls.Load() != 0 {
		t.Fatalf("status = %d origin = %d proxy = %d", response.StatusCode, originCalls.Load(), proxyCalls.Load())
	}
	host := stripPort(seenHost)
	if seenAuth != "Bearer device-token" || seenSNI != "example.com" || host != "example.com" {
		t.Fatalf("host = %q sni = %q auth = %q endpoint = %q", seenHost, seenSNI, seenAuth, endpoint)
	}
}

func TestPublicIP_拒绝本地和文档地址(t *testing.T) {
	rejected := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "100.64.1.1",
		"169.254.169.254", "192.0.2.1", "198.51.100.1", "203.0.113.1", "198.18.0.1",
		"0.0.0.0", "224.0.0.1", "255.255.255.255", "::1", "fc00::1", "fe80::1",
		"2001:db8::1", "100::1", "64:ff9b::1", "2002:7f00:1::", "::ffff:10.1.2.3",
	}
	for _, raw := range rejected {
		if publicIP(net.ParseIP(raw)) {
			t.Fatalf("accepted %s", raw)
		}
	}
	if !publicIP(net.ParseIP("1.1.1.1")) || !publicIP(net.ParseIP("2606:4700:4700::1111")) {
		t.Fatal("public addresses were rejected")
	}
}

func pinnedTestClient(t *testing.T, server *httptest.Server, token string) (string, nexusclient.Client) {
	t.Helper()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	endpoint := "https://example.com:" + strconv.Itoa(port)
	transport, err := packageTransport(endpoint, func(_ context.Context, host string) ([]net.IP, error) {
		if host != "example.com" {
			return nil, errors.New("unexpected host")
		}
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}, func(net.IP) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if cert := server.Certificate(); cert != nil {
		pool.AddCert(cert)
	}
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return endpoint, nexusclient.NewWithTransport(endpoint, token, transport)
}

func stripPort(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return host
}
