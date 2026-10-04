package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDirectLoopbackRequestAllowsLocalBrowser(t *testing.T) {
	for _, host := range []string{"127.0.0.1:27123", "localhost:27123", "[::1]:27123"} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:27123/internal/runtime/analytics", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Host = host
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		if !isDirectLoopbackRequest(req) {
			t.Fatalf("expected direct loopback request for host %q", host)
		}
	}
}

func TestDirectLoopbackRequestRejectsExternalPeerAndHost(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		host       string
	}{
		{name: "external peer", remoteAddr: "198.51.100.10:443", host: "127.0.0.1:27123"},
		{name: "public host", remoteAddr: "127.0.0.1:54321", host: "agentdock.example.com"},
		{name: "dns rebinding host", remoteAddr: "127.0.0.1:54321", host: "attacker.example"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:27123/internal/runtime/analytics", nil)
			req.RemoteAddr = test.remoteAddr
			req.Host = test.host
			if isDirectLoopbackRequest(req) {
				t.Fatalf("request unexpectedly accepted: remote=%q host=%q", test.remoteAddr, test.host)
			}
		})
	}
}

func TestDirectLoopbackRequestRejectsProxyHeaders(t *testing.T) {
	for _, header := range analyticsProxyHeaders {
		t.Run(header, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:27123/internal/runtime/analytics", nil)
			req.RemoteAddr = "127.0.0.1:54321"
			req.Host = "127.0.0.1:27123"
			req.Header.Set(header, "proxy-value")
			if isDirectLoopbackRequest(req) {
				t.Fatalf("request with %s unexpectedly accepted", header)
			}
		})
	}
}

func TestDirectLoopbackRequestRejectsCrossSiteBrowserRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:27123/internal/runtime/analytics", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "127.0.0.1:27123"
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if isDirectLoopbackRequest(req) {
		t.Fatal("cross-site request unexpectedly accepted")
	}
}
