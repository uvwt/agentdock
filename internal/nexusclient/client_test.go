package nexusclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClientSendsJSONHeadersAndBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/test" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer device-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := New(server.URL+"/", " device-token ")
	resp, err := client.Do(context.Background(), http.MethodPost, "v1/test", []byte(`{"value":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if client.Endpoint() != server.URL {
		t.Fatalf("endpoint = %q", client.Endpoint())
	}
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetCalls.Add(1)
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	client := New(redirect.URL, "device-token")
	resp, err := client.Do(context.Background(), http.MethodGet, "/v1/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if calls := targetCalls.Load(); calls != 0 {
		t.Fatalf("redirect target calls = %d", calls)
	}
}

func TestClientStreamPutsFixedLengthWithoutBuffering(t *testing.T) {
	payload := bytes.Repeat([]byte("zip"), 4096)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer device-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
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
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if !bytes.Equal(body, payload) {
			t.Errorf("body length = %d", len(body))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := New(server.URL, "device-token")
	resp, err := client.DoStream(context.Background(), http.MethodPut, "/v1/nodes/library/transfer/ticket", "application/zip", bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestReadBoundedBodyRejectsOversizedResponse(t *testing.T) {
	if _, err := ReadBoundedBody(strings.NewReader("12345"), 4); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("ReadBoundedBody() error = %v, want ErrResponseTooLarge", err)
	}
	data, err := ReadBoundedBody(strings.NewReader("1234"), 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "1234" {
		t.Fatalf("data = %q", data)
	}
}
