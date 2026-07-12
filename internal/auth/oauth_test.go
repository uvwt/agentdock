package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const oauthTestGrantID = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

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

func TestOAuthStoreRedeemDoesNotConsumeCodeOnBindingFailure(t *testing.T) {
	store := NewOAuthStore()
	original := OAuthCode{
		ClientID:    "client-id",
		RedirectURI: "https://client.example/callback",
		Challenge:   "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		Resource:    "https://agentdock.example/mcp",
	}
	code, err := store.Create(original)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.Redeem(code, original.ClientID, original.RedirectURI, strings.Repeat("x", 43), original.Resource); ok {
		t.Fatal("authorization code accepted with the wrong verifier")
	}
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	if _, ok, _ := store.Redeem(code, original.ClientID, original.RedirectURI, verifier, original.Resource); !ok {
		t.Fatal("binding failure consumed the authorization code")
	}
	if _, ok, replay := store.Redeem(code, original.ClientID, original.RedirectURI, verifier, original.Resource); ok || !replay {
		t.Fatal("successfully redeemed authorization code was accepted twice")
	}
}

func TestOAuthStoreRedeemRejectsAndRemovesExpiredCode(t *testing.T) {
	store := NewOAuthStore()
	code, err := store.Create(OAuthCode{})
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	expired := store.codes[code]
	expired.ExpiresAt = time.Now().Add(-time.Second)
	store.codes[code] = expired
	store.mu.Unlock()
	if _, ok, _ := store.Redeem(code, "", "", "", ""); ok {
		t.Fatal("expired authorization code was accepted")
	}
	if _, remains := store.codes[code]; remains {
		t.Fatal("expired authorization code was not removed")
	}
}

func TestOAuthStoreCreatePrunesExpiredCodesAtCapacity(t *testing.T) {
	store := NewOAuthStore()
	for index := 0; index < maxOAuthCodes; index++ {
		store.codes[fmt.Sprintf("expired-%d", index)] = OAuthCode{ExpiresAt: time.Now().Add(-time.Minute)}
	}
	if _, err := store.Create(OAuthCode{}); err != nil {
		t.Fatalf("Create() after expired code pruning: %v", err)
	}
	if len(store.codes) != 1 {
		t.Fatalf("authorization code count = %d, want 1", len(store.codes))
	}
}

func TestRevokedGrantPreventsLaterRefreshTokenIssuanceAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth", "state-v1.json")
	store, err := NewPersistentOAuthStore(path, "test-refresh-signing-key-32-bytes-long")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeGrant(oauthTestGrantID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := store.IssueRefreshToken("client", "https://agentdock.example/mcp", oauthTestGrantID, time.Hour); err == nil {
		t.Fatal("IssueRefreshToken() accepted a revoked grant")
	}
	reloaded, err := NewPersistentOAuthStore(path, "test-refresh-signing-key-32-bytes-long")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reloaded.IssueRefreshToken("client", "https://agentdock.example/mcp", oauthTestGrantID, time.Hour); err == nil {
		t.Fatal("IssueRefreshToken() accepted a persisted revoked grant")
	}
}

func TestPersistentOAuthStoreRotatesRefreshTokensAcrossReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth", "state-v1.json")
	store, err := NewPersistentOAuthStore(path, "test-refresh-signing-key-32-bytes-long")
	if err != nil {
		t.Fatal(err)
	}
	const clientID = "signed-client-id"
	const resource = "https://agentdock.example/mcp"
	if err := store.ActivateGrant(clientID, resource, oauthTestGrantID, time.Hour); err != nil {
		t.Fatal(err)
	}
	refreshToken, err := store.IssueRefreshToken(clientID, resource, oauthTestGrantID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), refreshToken) {
		t.Fatal("OAuth state persisted the raw refresh token")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("OAuth state mode = %o, want 600", info.Mode().Perm())
	}

	reloaded, err := NewPersistentOAuthStore(path, "test-refresh-signing-key-32-bytes-long")
	if err != nil {
		t.Fatal(err)
	}
	rotated, gotResource, _, ok, err := reloaded.RotateRefreshToken(refreshToken, clientID, resource, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || rotated == "" || rotated == refreshToken || gotResource != resource {
		t.Fatalf("rotation = token:%t resource:%q ok:%v", rotated != "", gotResource, ok)
	}
	reloadedAgain, err := NewPersistentOAuthStore(path, "test-refresh-signing-key-32-bytes-long")
	if err != nil {
		t.Fatal(err)
	}
	secondRotation, _, _, ok, err := reloadedAgain.RotateRefreshToken(rotated, clientID, resource, time.Hour)
	if err != nil || !ok || secondRotation == "" {
		t.Fatalf("persisted rotated token = token:%t ok:%v err:%v", secondRotation != "", ok, err)
	}
	if _, _, _, ok, err := reloadedAgain.RotateRefreshToken(refreshToken, clientID, resource, time.Hour); err != nil || ok {
		t.Fatalf("consumed refresh token replay = ok:%v err:%v", ok, err)
	}
	if _, _, _, ok, err := reloadedAgain.RotateRefreshToken(secondRotation, clientID, resource, time.Hour); err != nil || ok {
		t.Fatalf("refresh token family remained active after replay = ok:%v err:%v", ok, err)
	}
}

func TestRefreshGenerationRotationRemainsBounded(t *testing.T) {
	store := NewOAuthStore()
	const clientID = "client-id"
	const resource = "https://agentdock.example/mcp"
	if err := store.ActivateGrant(clientID, resource, oauthTestGrantID, time.Hour); err != nil {
		t.Fatal(err)
	}
	raw, err := store.IssueRefreshToken(clientID, resource, oauthTestGrantID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2500; index++ {
		next, _, _, ok, err := store.RotateRefreshToken(raw, clientID, resource, time.Hour)
		if err != nil || !ok {
			t.Fatalf("rotation %d failed: ok=%v err=%v", index, ok, err)
		}
		raw = next
	}
	if len(store.grants) != 1 || store.grants[oauthTestGrantID].CurrentGeneration != 2501 {
		t.Fatalf("grant state = %#v", store.grants)
	}
}

func TestPersistentOAuthStoreRegistersShortClientAcrossReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth", "state-v1.json")
	store, err := NewPersistentOAuthStore(path, "test-refresh-signing-key-32-bytes-long")
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

	reloaded, err := NewPersistentOAuthStore(path, "test-refresh-signing-key-32-bytes-long")
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

func TestPersistentOAuthStoreWritesVersionOneState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth", "state-v1.json")
	store, err := NewPersistentOAuthStore(path, "test-refresh-signing-key-32-bytes-long")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateGrant("client", "https://agentdock.example/mcp", oauthTestGrantID, time.Hour); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Version != 1 {
		t.Fatalf("OAuth state version = %d, want 1", state.Version)
	}
}

func TestPersistentOAuthStoreRejectsVersionTwoState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth", "state-v1.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":2,"tokens":{},"clients":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersistentOAuthStore(path, "test-refresh-signing-key-32-bytes-long"); err == nil || !strings.Contains(err.Error(), "unsupported OAuth state version 2") {
		t.Fatalf("NewPersistentOAuthStore() error = %v, want version 2 rejection", err)
	}
}

func TestPersistentOAuthStoreRejectsVersionThreeState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth", "state-v1.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":3,"grants":{},"clients":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersistentOAuthStore(path, "test-refresh-signing-key-32-bytes-long"); err == nil || !strings.Contains(err.Error(), "unsupported OAuth state version 3") {
		t.Fatalf("NewPersistentOAuthStore() error = %v, want version 3 rejection", err)
	}
}

func TestPersistentOAuthStoreRejectsVersionFourState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oauth", "state-v1.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":4,"grants":{},"clients":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersistentOAuthStore(path, "test-refresh-signing-key-32-bytes-long"); err == nil || !strings.Contains(err.Error(), "unsupported OAuth state version 4") {
		t.Fatalf("NewPersistentOAuthStore() error = %v, want version 4 rejection", err)
	}
}

func TestOAuthRefreshTokenRejectsClientAndResourceMismatchWithoutConsumption(t *testing.T) {
	store := NewOAuthStore()
	const clientID = "client-id"
	const resource = "https://agentdock.example/mcp"
	if err := store.ActivateGrant(clientID, resource, oauthTestGrantID, time.Hour); err != nil {
		t.Fatal(err)
	}
	refreshToken, err := store.IssueRefreshToken(clientID, resource, oauthTestGrantID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok, err := store.RotateRefreshToken(refreshToken, "other-client", resource, time.Hour); err != nil || ok {
		t.Fatalf("wrong client rotation = ok:%v err:%v", ok, err)
	}
	if _, _, _, ok, err := store.RotateRefreshToken(refreshToken, clientID, "https://agentdock.example/other", time.Hour); err != nil || ok {
		t.Fatalf("wrong resource rotation = ok:%v err:%v", ok, err)
	}
	if rotated, _, _, ok, err := store.RotateRefreshToken(refreshToken, clientID, resource, time.Hour); err != nil || !ok || rotated == "" {
		t.Fatalf("valid rotation after mismatches = token:%t ok:%v err:%v", rotated != "", ok, err)
	}
}

func TestPersistentOAuthStoreRejectsCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state-v1.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersistentOAuthStore(path, "test-refresh-signing-key-32-bytes-long"); err == nil || !strings.Contains(err.Error(), "decode OAuth state") {
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
	const grantID = oauthTestGrantID
	token, err := IssueToken(issuer, audience, grantID, key, time.Hour)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}
	if gotGrantID, ok := ValidateToken(token, issuer, audience, key); !ok || gotGrantID != grantID {
		t.Fatal("ValidateToken() rejected freshly issued token")
	}
	if _, ok := ValidateToken(token, "https://other.example", audience, key); ok {
		t.Fatal("ValidateToken() accepted wrong issuer")
	}
	if _, ok := ValidateToken(token, issuer, "https://other.example/mcp", key); ok {
		t.Fatal("ValidateToken() accepted wrong audience")
	}
	if _, ok := ValidateToken(token, issuer, audience, "wrong-key"); ok {
		t.Fatal("ValidateToken() accepted wrong signing key")
	}
	parts := strings.Split(token, ".")
	parts[1] = strings.Repeat("A", len(parts[1]))
	if _, ok := ValidateToken(strings.Join(parts, "."), issuer, audience, key); ok {
		t.Fatal("ValidateToken() accepted tampered signature")
	}
}

func TestIssueTokenValidatesRequiredInputs(t *testing.T) {
	tests := []struct {
		name     string
		issuer   string
		audience string
		grantID  string
		key      string
		ttl      time.Duration
	}{
		{name: "missing issuer", audience: "audience", grantID: "grant", key: "key", ttl: time.Hour},
		{name: "missing audience", issuer: "issuer", grantID: "grant", key: "key", ttl: time.Hour},
		{name: "missing grant", issuer: "issuer", audience: "audience", key: "key", ttl: time.Hour},
		{name: "missing key", issuer: "issuer", audience: "audience", grantID: "grant", ttl: time.Hour},
		{name: "non-positive ttl", issuer: "issuer", audience: "audience", grantID: "grant", key: "key"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := IssueToken(test.issuer, test.audience, test.grantID, test.key, test.ttl); err == nil {
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
				Issuer: issuer, Audience: audience, GrantID: "grant",
				IssuedAt: now.Add(-2 * time.Hour).Unix(), ExpiresAt: now.Add(-time.Hour).Unix(),
			},
		},
		{
			name: "missing issued at",
			claims: tokenClaims{
				Issuer: issuer, Audience: audience, GrantID: "grant", ExpiresAt: now.Add(time.Hour).Unix(),
			},
		},
		{
			name: "wrong audience",
			claims: tokenClaims{
				Issuer: issuer, Audience: "other", GrantID: "grant",
				IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
			},
		},
		{
			name: "issued too far in future",
			claims: tokenClaims{
				Issuer: issuer, Audience: audience, GrantID: "grant",
				IssuedAt: now.Add(2 * time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
			},
		},
		{
			name: "expires before issued at",
			claims: tokenClaims{
				Issuer: issuer, Audience: audience, GrantID: "grant",
				IssuedAt: now.Add(30 * time.Second).Unix(), ExpiresAt: now.Add(20 * time.Second).Unix(),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token := signClaimsForTest(t, test.claims, key)
			if _, ok := ValidateToken(token, issuer, audience, key); ok {
				t.Fatal("ValidateToken() accepted invalid claims")
			}
		})
	}
	for _, token := range []string{"", "one-part", "not-base64.signature", "e30.invalid-signature"} {
		if _, ok := ValidateToken(token, issuer, audience, key); ok {
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
	for _, header := range []string{"  Bearer secret  ", "bearer secret", "BEARER secret"} {
		request.Header.Set("Authorization", header)
		if !bearer.Authorized(request) {
			t.Fatalf("Authorized() rejected valid bearer header %q", header)
		}
	}
}

func TestEquivalentResourceURINormalizesSchemeHostAndDefaultPort(t *testing.T) {
	for _, pair := range [][2]string{
		{"HTTPS://MCP.EXAMPLE.COM/mcp", "https://mcp.example.com/mcp"},
		{"https://mcp.example.com:443/mcp", "https://mcp.example.com/mcp"},
		{"http://LOCALHOST:80/mcp", "http://localhost/mcp"},
	} {
		if !EquivalentResourceURI(pair[0], pair[1]) {
			t.Fatalf("resources should be equivalent: %q %q", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{
		{"https://mcp.example.com/mcp", "https://mcp.example.com/other"},
		{"https://mcp.example.com/mcp?a=1", "https://mcp.example.com/mcp?a=2"},
		{"https://user@mcp.example.com/mcp", "https://mcp.example.com/mcp"},
	} {
		if EquivalentResourceURI(pair[0], pair[1]) {
			t.Fatalf("resources should differ: %q %q", pair[0], pair[1])
		}
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
