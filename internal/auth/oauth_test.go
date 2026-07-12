package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRandomTokenUsesRequestedEntropyLength(t *testing.T) {
	if _, err := RandomToken(0); err == nil {
		t.Fatal("RandomToken(0) error = nil, want validation error")
	}

	first, err := RandomToken(32)
	if err != nil {
		t.Fatalf("RandomToken() error = %v", err)
	}
	second, err := RandomToken(32)
	if err != nil {
		t.Fatalf("RandomToken() second error = %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("decoded token length = %d, want 32", len(decoded))
	}
	if first == second {
		t.Fatal("two independently generated tokens are equal")
	}
}

func TestOAuthStoreConsumesAuthorizationCodeOnce(t *testing.T) {
	store := NewOAuthStore()
	original := OAuthCode{
		ClientID:    "client-id",
		RedirectURI: "https://client.example/callback",
		Challenge:   "challenge",
		State:       "state",
	}
	code, err := store.Create(original)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	consumed, ok := store.Consume(code)
	if !ok {
		t.Fatal("Consume() ok = false, want true")
	}
	if consumed.ClientID != original.ClientID ||
		consumed.RedirectURI != original.RedirectURI ||
		consumed.Challenge != original.Challenge ||
		consumed.State != original.State {
		t.Fatalf("consumed code = %#v, want original fields", consumed)
	}
	if !consumed.ExpiresAt.After(time.Now()) {
		t.Fatalf("ExpiresAt = %v, want future time", consumed.ExpiresAt)
	}
	if _, ok := store.Consume(code); ok {
		t.Fatal("authorization code was accepted more than once")
	}
}

func TestOAuthStoreRejectsExpiredAuthorizationCode(t *testing.T) {
	store := NewOAuthStore()
	code, err := store.Create(OAuthCode{ClientID: "client-id"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	store.mu.Lock()
	expired := store.codes[code]
	expired.ExpiresAt = time.Now().Add(-time.Second)
	store.codes[code] = expired
	store.mu.Unlock()

	if _, ok := store.Consume(code); ok {
		t.Fatal("expired authorization code was accepted")
	}
	store.mu.Lock()
	_, remains := store.codes[code]
	store.mu.Unlock()
	if remains {
		t.Fatal("expired authorization code was not removed")
	}
}

func TestPersistentOAuthStoreRotatesRefreshTokensAcrossReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth", "refresh-tokens.json")
	store, err := NewPersistentOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	const clientID = "signed-client-id"
	const resource = "https://agentdock.example/mcp"
	refreshToken, err := store.IssueRefreshToken(clientID, resource, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), refreshToken) {
		t.Fatal("refresh token state persisted the raw token")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("refresh token state mode = %o, want 600", info.Mode().Perm())
	}

	reloaded, err := NewPersistentOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	rotated, gotResource, ok, err := reloaded.RotateRefreshToken(refreshToken, clientID, resource, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || rotated == "" || rotated == refreshToken || gotResource != resource {
		t.Fatalf("rotation = token:%t resource:%q ok:%v", rotated != "", gotResource, ok)
	}
	if _, _, ok, err := reloaded.RotateRefreshToken(refreshToken, clientID, resource, time.Hour); err != nil || ok {
		t.Fatalf("consumed refresh token replay = ok:%v err:%v", ok, err)
	}

	reloadedAgain, err := NewPersistentOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	secondRotation, _, ok, err := reloadedAgain.RotateRefreshToken(rotated, clientID, "", time.Hour)
	if err != nil || !ok || secondRotation == "" {
		t.Fatalf("persisted rotated token = token:%t ok:%v err:%v", secondRotation != "", ok, err)
	}
}

func TestPersistentOAuthStoreRegistersShortClientAcrossReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth", "refresh-tokens.json")
	store, err := NewPersistentOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	clientID, err := store.RegisterClient(
		"Test Client",
		[]string{"https://client.example/callback"},
		[]string{"authorization_code", "refresh_token"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(clientID, shortClientPrefix) || len(clientID) > 64 {
		t.Fatalf("client ID = %q, want short persisted ID", clientID)
	}
	registration, ok := store.ClientRegistration(clientID)
	if !ok || registration.ClientName != "Test Client" {
		t.Fatalf("registration = %#v, ok=%v", registration, ok)
	}
	if !store.ValidateClientRedirect(clientID, "https://client.example/callback") ||
		!store.ClientAllowsGrant(clientID, "refresh_token") {
		t.Fatal("new client registration was not bound to redirect URI and grant")
	}
	secondClientID, err := store.RegisterClient(
		"Test Client",
		[]string{"https://client.example/callback"},
		[]string{"authorization_code", "refresh_token"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if secondClientID == clientID {
		t.Fatal("separate dynamic registrations reused the same client ID")
	}

	reloaded, err := NewPersistentOAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.ValidateClientID(clientID) ||
		!reloaded.ValidateClientRedirect(clientID, "https://client.example/callback") ||
		!reloaded.ClientAllowsGrant(clientID, "authorization_code") {
		t.Fatal("persisted client registration was not valid after reload")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("OAuth state mode = %o, want 600", info.Mode().Perm())
	}
}

func TestPersistentOAuthStoreRejectsVersionOneState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth", "refresh-tokens.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"tokens":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersistentOAuthStore(path); err == nil || !strings.Contains(err.Error(), "unsupported OAuth refresh token state version 1") {
		t.Fatalf("NewPersistentOAuthStore() error = %v, want version 1 rejection", err)
	}
}

func TestOAuthRefreshTokenRejectsClientAndResourceMismatchWithoutConsumption(t *testing.T) {
	store := NewOAuthStore()
	const clientID = "client-id"
	const resource = "https://agentdock.example/mcp"
	refreshToken, err := store.IssueRefreshToken(clientID, resource, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := store.RotateRefreshToken(refreshToken, "other-client", resource, time.Hour); err != nil || ok {
		t.Fatalf("wrong client rotation = ok:%v err:%v", ok, err)
	}
	if _, _, ok, err := store.RotateRefreshToken(refreshToken, clientID, "https://agentdock.example/other", time.Hour); err != nil || ok {
		t.Fatalf("wrong resource rotation = ok:%v err:%v", ok, err)
	}
	if rotated, _, ok, err := store.RotateRefreshToken(refreshToken, clientID, resource, time.Hour); err != nil || !ok || rotated == "" {
		t.Fatalf("valid rotation after mismatches = token:%t ok:%v err:%v", rotated != "", ok, err)
	}
}

func TestPersistentOAuthStoreRejectsCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refresh-tokens.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersistentOAuthStore(path); err == nil || !strings.Contains(err.Error(), "decode OAuth refresh token state") {
		t.Fatalf("NewPersistentOAuthStore() error = %v", err)
	}
}

func TestVerifyPKCEUsesS256(t *testing.T) {
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if !VerifyPKCE(verifier, challenge) {
		t.Fatal("VerifyPKCE() rejected RFC 7636 S256 example")
	}
	for _, invalidVerifier := range []string{
		verifier[:42],
		verifier + "!",
		strings.Repeat("a", 129),
		verifier + "x",
	} {
		if VerifyPKCE(invalidVerifier, challenge) {
			t.Fatalf("VerifyPKCE() accepted invalid verifier %q", invalidVerifier)
		}
	}
	for _, invalidChallenge := range []string{"", challenge[:42], challenge + "=", strings.Repeat("!", 43)} {
		if ValidPKCEChallenge(invalidChallenge) || VerifyPKCE(verifier, invalidChallenge) {
			t.Fatalf("invalid PKCE challenge accepted: %q", invalidChallenge)
		}
	}
	if !ValidPKCEChallenge(challenge) {
		t.Fatal("ValidPKCEChallenge() rejected RFC 7636 example")
	}
}

func TestIssueAndValidateToken(t *testing.T) {
	const issuer = "https://agentdock.example"
	const audience = issuer + "/mcp"
	const key = "test-signing-key"
	token, err := IssueToken(issuer, audience, key, time.Hour)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}
	if !ValidateToken(token, issuer, audience, key) {
		t.Fatal("ValidateToken() rejected freshly issued token")
	}
	if ValidateToken(token, "https://other.example", audience, key) {
		t.Fatal("ValidateToken() accepted wrong issuer")
	}
	if ValidateToken(token, issuer, "https://other.example/mcp", key) {
		t.Fatal("ValidateToken() accepted wrong audience")
	}
	if ValidateToken(token, issuer, audience, "wrong-key") {
		t.Fatal("ValidateToken() accepted wrong signing key")
	}
	parts := strings.Split(token, ".")
	parts[1] = strings.Repeat("A", len(parts[1]))
	if ValidateToken(strings.Join(parts, "."), issuer, audience, key) {
		t.Fatal("ValidateToken() accepted tampered signature")
	}
}

func TestIssueTokenValidatesRequiredInputs(t *testing.T) {
	tests := []struct {
		name     string
		issuer   string
		audience string
		key      string
		ttl      time.Duration
	}{
		{name: "missing issuer", audience: "audience", key: "key", ttl: time.Hour},
		{name: "missing audience", issuer: "issuer", key: "key", ttl: time.Hour},
		{name: "missing key", issuer: "issuer", audience: "audience", ttl: time.Hour},
		{name: "non-positive ttl", issuer: "issuer", audience: "audience", key: "key"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := IssueToken(test.issuer, test.audience, test.key, test.ttl); err == nil {
				t.Fatal("IssueToken() error = nil, want validation error")
			}
		})
	}
}

func TestValidateTokenRejectsInvalidClaims(t *testing.T) {
	const issuer = "https://agentdock.example"
	const audience = issuer + "/mcp"
	const key = "test-signing-key"
	now := time.Now()
	tests := []struct {
		name   string
		claims tokenClaims
	}{
		{
			name: "expired",
			claims: tokenClaims{
				Issuer: issuer, Audience: audience,
				IssuedAt: now.Add(-2 * time.Hour).Unix(), ExpiresAt: now.Add(-time.Hour).Unix(),
			},
		},
		{
			name: "missing issued at",
			claims: tokenClaims{
				Issuer: issuer, Audience: audience, ExpiresAt: now.Add(time.Hour).Unix(),
			},
		},
		{
			name: "wrong audience",
			claims: tokenClaims{
				Issuer: issuer, Audience: "other",
				IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
			},
		},
		{
			name: "issued too far in future",
			claims: tokenClaims{
				Issuer: issuer, Audience: audience,
				IssuedAt: now.Add(2 * time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
			},
		},
		{
			name: "expires before issued at",
			claims: tokenClaims{
				Issuer: issuer, Audience: audience,
				IssuedAt: now.Add(30 * time.Second).Unix(), ExpiresAt: now.Add(20 * time.Second).Unix(),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token := signClaimsForTest(t, test.claims, key)
			if ValidateToken(token, issuer, audience, key) {
				t.Fatal("ValidateToken() accepted invalid claims")
			}
		})
	}
	for _, token := range []string{"", "one-part", "not-base64.signature", "e30.invalid-signature"} {
		if ValidateToken(token, issuer, audience, key) {
			t.Fatalf("ValidateToken(%q) = true, want false", token)
		}
	}
}

func TestAppendQueryPreservesExistingValues(t *testing.T) {
	got := AppendQuery("https://client.example/callback?existing=1", url.Values{
		"code":  {"code-value"},
		"state": {"state-value"},
	})
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if parsed.Query().Get("existing") != "1" ||
		parsed.Query().Get("code") != "code-value" ||
		parsed.Query().Get("state") != "state-value" {
		t.Fatalf("query = %#v", parsed.Query())
	}
	if got := AppendQuery("://bad-url", url.Values{"code": {"value"}}); got != "://bad-url" {
		t.Fatalf("AppendQuery() invalid URL = %q, want original", got)
	}
}

func TestBearerAuthorization(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "https://agentdock.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !(Bearer{}).Authorized(request) {
		t.Fatal("disabled bearer auth rejected request")
	}

	bearer := Bearer{Token: "secret"}
	for _, header := range []string{"", "Bearer", "Basic secret", "Bearer wrong"} {
		request.Header.Set("Authorization", header)
		if bearer.Authorized(request) {
			t.Fatalf("Authorized() accepted header %q", header)
		}
	}
	request.Header.Set("Authorization", "  Bearer secret  ")
	if !bearer.Authorized(request) {
		t.Fatal("Authorized() rejected valid bearer token with outer whitespace")
	}
}

func signClaimsForTest(t *testing.T, claims tokenClaims, key string) string {
	t.Helper()
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(key))
	if _, err := mac.Write([]byte(encoded)); err != nil {
		t.Fatal(err)
	}
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
