package resourcelibrary

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func TestAuthorizeCloudUpload_只允许配对原点上的传输路由(t *testing.T) {
	endpoint := "https://1.1.1.1"
	rejected := []string{
		"http://1.1.1.1/v1/nodes/library/transfer/ticket-ok",
		"https://127.0.0.1/v1/nodes/library/transfer/ticket-ok",
		"https://10.1.2.3/v1/nodes/library/transfer/ticket-ok",
		"https://evil.example/v1/nodes/library/transfer/ticket-ok",
		"https://user:token@1.1.1.1/v1/nodes/library/transfer/ticket-ok",
		"https://1.1.1.1/v1/nodes/library/transfer/../ticket-ok",
		"https://1.1.1.1/v1/packages/ticket-ok",
		"https://1.1.1.1/v1/nodes/library/transfer/ticket-ok?sig=1",
		"https://1.1.1.1:8443/v1/nodes/library/transfer/ticket-ok",
	}
	for _, rawURL := range rejected {
		if _, err := AuthorizeCloudUpload(endpoint, rawURL); err == nil {
			t.Fatalf("accepted %s", rawURL)
		}
	}
	requestURI, err := AuthorizeCloudUpload(endpoint, "https://1.1.1.1/v1/nodes/library/transfer/ticket-ok")
	if err != nil {
		t.Fatal(err)
	}
	if requestURI != "/v1/nodes/library/transfer/ticket-ok" {
		t.Fatalf("request URI = %q", requestURI)
	}
}

func TestUploadPackage_其他Origin和本机地址不会发出请求(t *testing.T) {
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

	archive := filepathTemp(t)
	if err := os.WriteFile(archive, []byte("zip-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	endpoint := "http://" + listener.Addr().String()
	err = UploadPackage(context.Background(), endpoint, "device-token", endpoint+"/v1/nodes/library/transfer/ticket-ok", archive, 9)
	if err == nil || calls.Load() != 0 {
		t.Fatalf("local upload error = %v calls = %d", err, calls.Load())
	}
	err = UploadPackage(context.Background(), "https://1.1.1.1", "device-token", "https://evil.example/v1/nodes/library/transfer/ticket-ok", archive, 9)
	if err == nil || calls.Load() != 0 {
		t.Fatalf("other origin error = %v calls = %d", err, calls.Load())
	}
}

func TestUploadAuthorizedPath_流式PUT且不跟随重定向(t *testing.T) {
	payload := []byte("portable-zip-body")
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetCalls.Add(1)
	}))
	defer target.Close()

	upload := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/v1/nodes/library/transfer/ticket-ok" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer device-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.TLS == nil || r.TLS.ServerName != "example.com" || net.ParseIP(stripPort(r.Host)) != nil {
			t.Errorf("host = %q tls = %#v", r.Host, r.TLS)
		}
		if r.Header.Get("Content-Type") != "application/zip" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		if r.ContentLength != int64(len(payload)) {
			t.Errorf("Content-Length = %d", r.ContentLength)
		}
		if len(r.TransferEncoding) != 0 {
			t.Errorf("Transfer-Encoding = %v", r.TransferEncoding)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != string(payload) {
			t.Errorf("body = %q %v", body, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upload.Close()

	archive := filepathTemp(t)
	if err := os.WriteFile(archive, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	_, client := pinnedTestClient(t, upload, "device-token")
	if err := uploadWith(context.Background(), client, "/v1/nodes/library/transfer/ticket-ok", file, int64(len(payload))); err != nil {
		t.Fatal(err)
	}

	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen.zip", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	redirectFile, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer redirectFile.Close()
	_, redirectClient := pinnedTestClient(t, redirect, "device-token")
	err = uploadWith(context.Background(), redirectClient, "/v1/nodes/library/transfer/ticket-ok", redirectFile, int64(len(payload)))
	if err == nil || !errorCode(err, "UPLOAD_REDIRECT_REJECTED") || targetCalls.Load() != 0 {
		t.Fatalf("redirect error = %v target calls = %d", err, targetCalls.Load())
	}
	if err.Error() == "device-token" || containsToken(err.Error()) {
		t.Fatalf("upload error leaked a token: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var cancelledCalls atomic.Int32
	blocked := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		cancelledCalls.Add(1)
	}))
	defer blocked.Close()
	cancelledFile, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelledFile.Close()
	_, blockedClient := pinnedTestClient(t, blocked, "device-token")
	err = uploadWith(ctx, blockedClient, "/v1/nodes/library/transfer/ticket-ok", cancelledFile, int64(len(payload)))
	if err == nil || !errorCode(err, "UPLOAD_TIMEOUT") || cancelledCalls.Load() != 0 {
		t.Fatalf("cancelled upload error = %v calls = %d", err, cancelledCalls.Load())
	}
}

func containsToken(message string) bool {
	return len(message) > 0 && (containsString([]string{message}, "device-token") || containsString([]string{message}, "AGENTDOCK_"))
}
