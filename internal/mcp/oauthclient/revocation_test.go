package oauthclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

type gatedTokenRoundTripper func(*http.Request) (*http.Response, error)

func (f gatedTokenRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClearDuringExchangeDoesNotRecreateGrant(t *testing.T) {
	fake := newFakeOAuthService(t)
	manager := prepareOAuthManager(t, fake, t.TempDir())
	started, gate := make(chan struct{}), make(chan struct{})
	manager.httpClient = &http.Client{Timeout: time.Second * 5, Transport: gatedTokenRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/token" {
			close(started)
			<-gate
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	result, done, err := manager.Begin(context.Background(), "cloudflare", "cloudflare", fake.endpoint(), CallbackLocal)
	if err != nil {
		t.Fatal(err)
	}
	authURL, _ := url.Parse(result.AuthorizationURL)
	if err = manager.DeliverCallback(CallbackResult{State: authURL.Query().Get("state"), Code: "code-1", Issuer: fake.server.URL}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("exchange did not start")
	}
	clearErr := manager.Clear("cloudflare")
	close(gate)
	if clearErr != nil {
		t.Fatal(clearErr)
	}
	select {
	case err = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("exchange did not finish")
	}
	_, loadErr := manager.store.loadGrant("cloudflare")
	if !errors.Is(loadErr, os.ErrNotExist) {
		t.Errorf("authorization resurrected after Clear returned: exchange error=%v, grant load error=%v", err, loadErr)
	}
}

func TestClearDuringPreparationCancelsNewFlow(t *testing.T) {
	fake := newFakeOAuthService(t)
	started, gate := make(chan struct{}, 1), make(chan struct{})
	fake.registrationStart, fake.registrationGate = started, gate
	manager := prepareOAuthManager(t, fake, t.TempDir())
	done := make(chan error, 1)
	go func() {
		_, _, err := manager.Begin(context.Background(), "cloudflare", "cloudflare", fake.endpoint(), CallbackLocal)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(gate)
		t.Fatal("registration did not start")
	}
	err := manager.Clear("cloudflare")
	close(gate)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		var flowErr *FlowError
		if !errors.As(err, &flowErr) || flowErr.Code != "MCP_AUTH_CANCELLED" {
			t.Fatalf("Begin after Clear = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Begin did not finish")
	}
}

func TestClearDuringRefreshDoesNotRecreateGrant(t *testing.T) {
	fake := newFakeOAuthService(t)
	manager := prepareOAuthManager(t, fake, t.TempDir())
	authorizeFakeService(t, manager, fake)
	grant, err := manager.store.loadGrant("cloudflare")
	if err != nil {
		t.Fatal(err)
	}
	grant.Expiry = time.Now().Add(-time.Minute)
	if err := manager.store.saveGrant("cloudflare", grant); err != nil {
		t.Fatal(err)
	}
	started, gate := make(chan struct{}), make(chan struct{})
	manager.httpClient.Transport = gatedTokenRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/token" {
			close(started)
			<-gate
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	source, err := manager.tokenSource("cloudflare", fake.endpoint())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := source.Token(); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(gate)
		t.Fatal("refresh did not start")
	}
	err = manager.Clear("cloudflare")
	close(gate)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		var required *AuthRequiredError
		if !errors.As(err, &required) {
			t.Fatalf("revoked TokenSource error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not finish")
	}
	if _, err := manager.store.loadGrant("cloudflare"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("grant recreated: %v", err)
	}
}

func TestClearedTokenSourceDoesNotContactProvider(t *testing.T) {
	fake := newFakeOAuthService(t)
	manager := prepareOAuthManager(t, fake, t.TempDir())
	authorizeFakeService(t, manager, fake)
	grant, err := manager.store.loadGrant("cloudflare")
	if err != nil {
		t.Fatal(err)
	}
	grant.Expiry = time.Now().Add(-time.Minute)
	if err := manager.store.saveGrant("cloudflare", grant); err != nil {
		t.Fatal(err)
	}
	source, err := manager.tokenSource("cloudflare", fake.endpoint())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Clear("cloudflare"); err != nil {
		t.Fatal(err)
	}
	_, err = source.Token()
	var required *AuthRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("cleared source returned %v", err)
	}
	if count := fake.refreshCount(); count != 0 {
		t.Fatalf("cleared source contacted provider %d time(s)", count)
	}
}
