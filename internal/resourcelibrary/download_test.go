package resourcelibrary

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestAuthorizeCloudDownload_拒绝其他云和本地地址(t *testing.T) {
	endpoint := "https://1.1.1.1"
	tests := []string{
		"http://1.1.1.1/pkg.zip",
		"https://127.0.0.1/pkg.zip",
		"https://localhost/pkg.zip",
		"https://169.254.169.254/latest/meta-data",
		"https://10.1.2.3/pkg.zip",
		"https://evil.example/pkg.zip",
		"https://1.1.1.1.evil.example/pkg.zip",
		"https://user:token@1.1.1.1/pkg.zip",
		"https://1.1.1.1/../../pkg.zip",
		"file:///tmp/pkg.zip",
	}
	for _, rawURL := range tests {
		if _, err := AuthorizeCloudDownload(endpoint, rawURL); err == nil {
			t.Fatalf("accepted %s", rawURL)
		}
	}
	requestURI, err := AuthorizeCloudDownload(endpoint, "https://1.1.1.1/packages/demo.zip?sig=1")
	if err != nil {
		t.Fatal(err)
	}
	if requestURI != "/packages/demo.zip?sig=1" {
		t.Fatalf("request URI = %q", requestURI)
	}
}

func TestFetchPackage_本地地址不会发出请求(t *testing.T) {
	var calls atomic.Int32
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	})}
	go server.Serve(listener)
	defer server.Close()

	endpoint := "http://" + listener.Addr().String()
	_, err = FetchPackage(context.Background(), endpoint, "device-token", endpoint+"/pkg.zip", filepathTemp(t), 1024)
	if err == nil {
		t.Fatal("local download was accepted")
	}
	if calls.Load() != 0 {
		t.Fatalf("local listener received %d requests", calls.Load())
	}
}

func TestDownloadAuthorizedPath_不跟随重定向并限制大小(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetCalls.Add(1)
	}))
	defer target.Close()

	redirectServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "" || net.ParseIP(stripPort(r.Host)) != nil {
			t.Errorf("host = %q", r.Host)
		}
		if r.TLS == nil || r.TLS.ServerName != "example.com" {
			t.Errorf("sni = %#v", r.TLS)
		}
		if r.Header.Get("Authorization") != "Bearer device-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		http.Redirect(w, r, target.URL+"/pkg.zip", http.StatusTemporaryRedirect)
	}))
	defer redirectServer.Close()
	_, client := pinnedTestClient(t, redirectServer, "device-token")
	_, err := downloadWith(context.Background(), client, "/pkg.zip", filepathTemp(t), 32)
	if err == nil {
		t.Fatal("redirect was accepted")
	}
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target calls = %d", targetCalls.Load())
	}

	large := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer large.Close()
	_, client = pinnedTestClient(t, large, "device-token")
	_, err = downloadWith(context.Background(), client, "/pkg.zip", filepathTemp(t), 4)
	if err == nil {
		t.Fatal("oversized download was accepted")
	}
}

func filepathTemp(t *testing.T) string {
	t.Helper()
	return t.TempDir() + "/package.zip"
}
