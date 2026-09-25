package oauthclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type fakeOAuthService struct {
	server *httptest.Server

	mu                sync.Mutex
	registrations     int
	refreshes         int
	invalidRefresh    bool
	lastRegistration  oauthex.ClientRegistrationMetadata
	registrationStart chan struct{}
	registrationGate  chan struct{}
}

func newFakeOAuthService(t *testing.T) *fakeOAuthService {
	t.Helper()
	fake := &fakeOAuthService{}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeOAuthService) endpoint() string { return f.server.URL + "/mcp" }

func (f *fakeOAuthService) challenge() []string {
	return []string{fmt.Sprintf(`Bearer resource_metadata="%s/resource-metadata"`, f.server.URL)}
}

func (f *fakeOAuthService) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/resource-metadata":
		writeTestJSON(w, http.StatusOK, map[string]any{
			"resource":              f.endpoint(),
			"authorization_servers": []string{f.server.URL},
		})
	case "/.well-known/oauth-authorization-server":
		writeTestJSON(w, http.StatusOK, map[string]any{
			"issuer":                                         f.server.URL,
			"authorization_endpoint":                         f.server.URL + "/authorize",
			"token_endpoint":                                 f.server.URL + "/token",
			"registration_endpoint":                          f.server.URL + "/register",
			"response_types_supported":                       []string{"code"},
			"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
			"token_endpoint_auth_methods_supported":          []string{"none"},
			"code_challenge_methods_supported":               []string{"S256"},
			"authorization_response_iss_parameter_supported": true,
		})
	case "/register":
		var meta oauthex.ClientRegistrationMetadata
		if err := json.NewDecoder(r.Body).Decode(&meta); err != nil {
			writeTestJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_client_metadata"})
			return
		}
		f.mu.Lock()
		f.registrations++
		registrationNumber := f.registrations
		f.lastRegistration = meta
		start, gate := f.registrationStart, f.registrationGate
		f.mu.Unlock()
		if start != nil {
			select {
			case start <- struct{}{}:
			default:
			}
		}
		if gate != nil {
			<-gate
		}
		writeTestJSON(w, http.StatusCreated, map[string]any{
			"client_id":                  fmt.Sprintf("client-%d", registrationNumber),
			"redirect_uris":              meta.RedirectURIs,
			"token_endpoint_auth_method": "none",
			"grant_types":                meta.GrantTypes,
			"response_types":             meta.ResponseTypes,
			"application_type":           meta.ApplicationType,
		})
	case "/token":
		if err := r.ParseForm(); err != nil {
			writeTestJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request"})
			return
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "code-1" || r.Form.Get("code_verifier") == "" || r.Form.Get("resource") != f.endpoint() {
				writeTestJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
				return
			}
			writeTestJSON(w, http.StatusOK, map[string]any{
				"access_token": "access-1", "refresh_token": "refresh-1", "token_type": "Bearer", "expires_in": 3600,
			})
		case "refresh_token":
			if r.Form.Get("resource") != f.endpoint() {
				writeTestJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_target"})
				return
			}
			f.mu.Lock()
			f.refreshes++
			invalid := f.invalidRefresh
			f.mu.Unlock()
			if invalid {
				writeTestJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
				return
			}
			if r.Form.Get("refresh_token") != "refresh-1" {
				writeTestJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
				return
			}
			writeTestJSON(w, http.StatusOK, map[string]any{
				"access_token": "access-2", "refresh_token": "refresh-2", "token_type": "Bearer", "expires_in": 3600,
			})
		default:
			writeTestJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported_grant_type"})
		}
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeOAuthService) registrationCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.registrations
}

func (f *fakeOAuthService) refreshCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshes
}

func writeTestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func prepareOAuthManager(t *testing.T, fake *fakeOAuthService, home string) *Manager {
	t.Helper()
	manager, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetCallback(CallbackOption{
		ID: CallbackLocal, Label: "本机", RedirectURL: "http://127.0.0.1:8765/oauth/mcp/callback",
	}); err != nil {
		t.Fatal(err)
	}
	manager.RecordChallenge("cloudflare", fake.endpoint(), fake.challenge())
	return manager
}

func authorizeFakeService(t *testing.T, manager *Manager, fake *fakeOAuthService) {
	t.Helper()
	result, done, err := manager.Begin(context.Background(), "cloudflare", "cloudflare", fake.endpoint(), CallbackLocal)
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthorizationURL == "" || done == nil {
		t.Fatalf("authorization result = %#v", result)
	}
	authURL, err := url.Parse(result.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	query := authURL.Query()
	if query.Get("scope") != "" {
		t.Fatalf("authorization scope = %q, want omitted", query.Get("scope"))
	}
	if query.Get("resource") != fake.endpoint() {
		t.Fatalf("authorization resource = %q", query.Get("resource"))
	}
	if query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization PKCE query = %#v", query)
	}
	if query.Get("redirect_uri") != "http://127.0.0.1:8765/oauth/mcp/callback" {
		t.Fatalf("authorization redirect_uri = %q", query.Get("redirect_uri"))
	}
	if err := manager.DeliverCallback(CallbackResult{State: query.Get("state"), Code: "code-1", Issuer: fake.server.URL}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("authorization flow did not complete")
	}
}

func TestManagerAuthorizationPersistsAndRefreshesRotatedToken(t *testing.T) {
	fake := newFakeOAuthService(t)
	home := t.TempDir()
	manager := prepareOAuthManager(t, fake, home)
	authorizeFakeService(t, manager, fake)

	grant, err := manager.store.loadGrant("cloudflare")
	if err != nil {
		t.Fatal(err)
	}
	if grant.AccessToken != "access-1" || grant.RefreshToken != "refresh-1" || grant.Issuer != fake.server.URL || grant.Resource != fake.endpoint() {
		t.Fatalf("persisted grant = %#v", grant)
	}
	if fake.registrationCount() != 1 {
		t.Fatalf("DCR registrations = %d, want 1", fake.registrationCount())
	}
	fake.mu.Lock()
	registration := fake.lastRegistration
	fake.mu.Unlock()
	if registration.Scope != "" {
		t.Fatalf("DCR scope = %q, want omitted", registration.Scope)
	}
	if len(registration.RedirectURIs) != 1 || registration.RedirectURIs[0] != "http://127.0.0.1:8765/oauth/mcp/callback" {
		t.Fatalf("DCR redirects = %#v", registration.RedirectURIs)
	}

	// 模拟 AgentDock 重启和 access token 过期。新的 Manager 必须复用磁盘上的
	// DCR client + refresh token，并把 rotated refresh token 原子写回同一 grant。
	grant.Expiry = time.Now().Add(-time.Minute)
	if err := manager.store.saveGrant("cloudflare", grant); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	source, err := restarted.tokenSource("cloudflare", fake.endpoint())
	if err != nil {
		t.Fatal(err)
	}
	if source == nil {
		t.Fatal("token source missing after restart")
	}
	token, err := source.Token()
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "access-2" || token.RefreshToken != "refresh-2" {
		t.Fatalf("refreshed token = %#v", token)
	}
	rotated, err := restarted.store.loadGrant("cloudflare")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.AccessToken != "access-2" || rotated.RefreshToken != "refresh-2" {
		t.Fatalf("persisted rotated grant = %#v", rotated)
	}
	if fake.refreshCount() != 1 || fake.registrationCount() != 1 {
		t.Fatalf("refreshes=%d registrations=%d", fake.refreshCount(), fake.registrationCount())
	}
}

func TestInvalidGrantRemovesGrantAndReturnsAuthRequired(t *testing.T) {
	fake := newFakeOAuthService(t)
	home := t.TempDir()
	manager := prepareOAuthManager(t, fake, home)
	authorizeFakeService(t, manager, fake)
	grant, err := manager.store.loadGrant("cloudflare")
	if err != nil {
		t.Fatal(err)
	}
	grant.Expiry = time.Now().Add(-time.Minute)
	if err := manager.store.saveGrant("cloudflare", grant); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.invalidRefresh = true
	fake.mu.Unlock()
	source, err := manager.tokenSource("cloudflare", fake.endpoint())
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Token()
	var authRequired *AuthRequiredError
	if !errors.As(err, &authRequired) {
		t.Fatalf("Token() error = %T %v, want AuthRequiredError", err, err)
	}
	if _, err := manager.store.loadGrant("cloudflare"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("grant after invalid_grant error = %v, want removed", err)
	}
}

func TestBeginWithMultipleCallbacksReturnsChoiceBeforeDiscovery(t *testing.T) {
	manager, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	callbacks := []CallbackOption{
		{ID: CallbackLocal, Label: "本机", RedirectURL: "http://127.0.0.1:8765/oauth/mcp/callback"},
		{ID: CallbackAgentDock, Label: "AgentDock", RedirectURL: "https://agent.example.test/oauth/mcp/callback"},
	}
	for _, callback := range callbacks {
		if err := manager.SetCallback(callback); err != nil {
			t.Fatal(err)
		}
	}
	result, done, err := manager.Begin(context.Background(), "demo", "demo", "https://unreachable.example.test/mcp", "")
	if err != nil {
		t.Fatal(err)
	}
	if done != nil || result.AuthorizationURL != "" || len(result.CallbackOptions) != 2 {
		t.Fatalf("choice result = %#v done=%v", result, done)
	}
}

func TestConcurrentBeginDoesNotDuplicateDCR(t *testing.T) {
	fake := newFakeOAuthService(t)
	fake.mu.Lock()
	fake.registrationStart = make(chan struct{}, 1)
	fake.registrationGate = make(chan struct{})
	start, gate := fake.registrationStart, fake.registrationGate
	fake.mu.Unlock()
	manager := prepareOAuthManager(t, fake, t.TempDir())

	firstDone := make(chan error, 1)
	go func() {
		_, _, err := manager.Begin(context.Background(), "cloudflare", "cloudflare", fake.endpoint(), CallbackLocal)
		firstDone <- err
	}()
	select {
	case <-start:
	case <-time.After(3 * time.Second):
		t.Fatal("first authorization did not reach DCR")
	}
	_, _, err := manager.Begin(context.Background(), "cloudflare", "cloudflare", fake.endpoint(), CallbackLocal)
	var flowErr *FlowError
	if !errors.As(err, &flowErr) || flowErr.Code != "MCP_AUTH_IN_PROGRESS" {
		t.Fatalf("concurrent Begin error = %T %v", err, err)
	}
	close(gate)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if fake.registrationCount() != 1 {
		t.Fatalf("DCR registrations = %d, want 1", fake.registrationCount())
	}
	_ = manager.Clear("cloudflare")
}

func TestExplicitResourceMetadataFailureDoesNotDowngradeToGuessedMetadata(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/broken-resource-metadata":
			http.Error(w, "broken", http.StatusInternalServerError)
		case "/.well-known/oauth-protected-resource/mcp":
			writeTestJSON(w, http.StatusOK, map[string]any{"resource": server.URL + "/mcp", "authorization_servers": []string{server.URL}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	_, err := discoverAuthorization(context.Background(), server.Client(), server.URL+"/mcp", []string{
		fmt.Sprintf(`Bearer resource_metadata="%s/broken-resource-metadata"`, server.URL),
	})
	if err == nil || !strings.Contains(err.Error(), "challenged protected resource metadata") {
		t.Fatalf("discoverAuthorization() error = %v", err)
	}
}

func TestHandlerOnlyTreatsOAuthChallengesAsAuthorizationRequired(t *testing.T) {
	manager, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler := manager.Handler("demo", "demo", "https://example.test/mcp")

	plainForbidden := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     make(http.Header),
		Body:       http.NoBody,
	}
	err = handler.Authorize(context.Background(), nil, plainForbidden)
	var authRequired *AuthRequiredError
	if errors.As(err, &authRequired) {
		t.Fatalf("plain 403 became AuthRequiredError: %v", err)
	}
	if got := manager.Status("demo", "https://example.test/mcp"); got != StatusUnauthorized {
		t.Fatalf("status after plain 403 = %q", got)
	}

	stepUp := &http.Response{
		StatusCode: http.StatusForbidden,
		Header: http.Header{"Www-Authenticate": []string{
			`Bearer error="insufficient_scope", scope="admin", resource_metadata="https://example.test/.well-known/oauth-protected-resource/mcp"`,
		}},
		Body: http.NoBody,
	}
	err = handler.Authorize(context.Background(), nil, stepUp)
	if !errors.As(err, &authRequired) {
		t.Fatalf("step-up error = %T %v, want AuthRequiredError", err, err)
	}
	if got := manager.Status("demo", "https://example.test/mcp"); got != StatusAuthRequired {
		t.Fatalf("status after step-up challenge = %q", got)
	}
}

func TestStepUpAuthorizationKeepsPreviouslyGrantedScopes(t *testing.T) {
	fake := newFakeOAuthService(t)
	manager := prepareOAuthManager(t, fake, t.TempDir())
	if err := manager.store.saveGrant("cloudflare", Grant{
		Endpoint:      fake.endpoint(),
		Resource:      fake.endpoint(),
		Issuer:        fake.server.URL,
		AccessToken:   "old-access",
		GrantedScopes: []string{"read"},
	}); err != nil {
		t.Fatal(err)
	}
	manager.RecordChallenge("cloudflare", fake.endpoint(), []string{
		fmt.Sprintf(`Bearer error="insufficient_scope", scope="admin", resource_metadata="%s/resource-metadata"`, fake.server.URL),
	})

	result, _, err := manager.Begin(context.Background(), "cloudflare", "cloudflare", fake.endpoint(), CallbackLocal)
	if err != nil {
		t.Fatal(err)
	}
	authURL, err := url.Parse(result.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(authURL.Query().Get("scope")); len(got) != 2 || got[0] != "read" || got[1] != "admin" {
		t.Fatalf("step-up scope = %#v, want [read admin]", got)
	}
	if err := manager.Clear("cloudflare"); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentDifferentServersReuseSingleDCRClient(t *testing.T) {
	fake := newFakeOAuthService(t)
	fake.mu.Lock()
	fake.registrationStart = make(chan struct{}, 1)
	fake.registrationGate = make(chan struct{})
	start, gate := fake.registrationStart, fake.registrationGate
	fake.mu.Unlock()
	manager, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SetCallback(CallbackOption{ID: CallbackLocal, Label: "本机", RedirectURL: "http://127.0.0.1:8765/oauth/mcp/callback"}); err != nil {
		t.Fatal(err)
	}
	manager.RecordChallenge("server-a", fake.endpoint(), fake.challenge())
	manager.RecordChallenge("server-b", fake.endpoint(), fake.challenge())

	type beginOutcome struct {
		storage string
		err     error
	}
	outcomes := make(chan beginOutcome, 2)
	for _, storage := range []string{"server-a", "server-b"} {
		storage := storage
		go func() {
			_, _, err := manager.Begin(context.Background(), storage, storage, fake.endpoint(), CallbackLocal)
			outcomes <- beginOutcome{storage: storage, err: err}
		}()
	}
	select {
	case <-start:
	case <-time.After(3 * time.Second):
		t.Fatal("authorization did not reach DCR")
	}
	close(gate)
	for range 2 {
		outcome := <-outcomes
		if outcome.err != nil {
			t.Fatalf("Begin(%s): %v", outcome.storage, outcome.err)
		}
	}
	if fake.registrationCount() != 1 {
		t.Fatalf("DCR registrations = %d, want 1 shared client", fake.registrationCount())
	}
	_ = manager.Clear("server-a")
	_ = manager.Clear("server-b")
}

func TestGrantPathPrefixesWindowsReservedStorageNames(t *testing.T) {
	store, err := newStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, storageKey := range []string{"con", "nul", "aux", "com1", "lpt1"} {
		path, err := store.grantPath(storageKey)
		if err != nil {
			t.Fatalf("grantPath(%q): %v", storageKey, err)
		}
		if got, want := filepath.Base(path), "grant-"+storageKey+".json"; got != want {
			t.Fatalf("grantPath(%q) basename = %q, want %q", storageKey, got, want)
		}
	}
}
