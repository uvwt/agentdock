package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/atomicfile"
)

const (
	dynamicClientPrefix = "adcr."
	refreshStateVersion = 1
	maxRefreshStateSize = 1 << 20
	maxRefreshTokens    = 1024
)

type OAuthStore struct {
	mu            sync.Mutex
	codes         map[string]OAuthCode
	refreshTokens map[string]OAuthRefreshToken
	refreshPath   string
}

type OAuthCode struct {
	ClientID    string
	RedirectURI string
	Challenge   string
	State       string
	Resource    string
	ExpiresAt   time.Time
}

type OAuthRefreshToken struct {
	ClientID  string `json:"client_id"`
	Resource  string `json:"resource"`
	IssuedAt  int64  `json:"issued_at"`
	ExpiresAt int64  `json:"expires_at"`
}

type refreshTokenState struct {
	Version int                          `json:"version"`
	Tokens  map[string]OAuthRefreshToken `json:"tokens"`
}

type tokenClaims struct {
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

type clientRegistration struct {
	Version      int      `json:"v"`
	RedirectURIs []string `json:"redirect_uris"`
	GrantTypes   []string `json:"grant_types"`
	IssuedAt     int64    `json:"iat"`
	Nonce        string   `json:"nonce,omitempty"`
}

func NewOAuthStore() *OAuthStore {
	return &OAuthStore{
		codes:         map[string]OAuthCode{},
		refreshTokens: map[string]OAuthRefreshToken{},
	}
}

func NewPersistentOAuthStore(path string) (*OAuthStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("OAuth refresh token state path is required")
	}
	store := NewOAuthStore()
	store.refreshPath = path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read OAuth refresh token state: %w", err)
	}
	if len(data) > maxRefreshStateSize {
		return nil, fmt.Errorf("OAuth refresh token state exceeds %d bytes", maxRefreshStateSize)
	}
	var state refreshTokenState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode OAuth refresh token state: %w", err)
	}
	if state.Version != refreshStateVersion {
		return nil, fmt.Errorf("unsupported OAuth refresh token state version %d", state.Version)
	}
	if state.Tokens == nil {
		state.Tokens = map[string]OAuthRefreshToken{}
	}
	now := time.Now().Unix()
	pruned := false
	for digest, token := range state.Tokens {
		if !validStoredRefreshToken(digest, token, now) {
			delete(state.Tokens, digest)
			pruned = true
		}
	}
	if len(state.Tokens) > maxRefreshTokens {
		return nil, fmt.Errorf("OAuth refresh token state contains %d entries, maximum is %d", len(state.Tokens), maxRefreshTokens)
	}
	store.refreshTokens = state.Tokens
	if pruned {
		store.mu.Lock()
		err = store.persistRefreshTokensLocked()
		store.mu.Unlock()
		if err != nil {
			return nil, err
		}
	}
	return store, nil
}

func (s *OAuthStore) Create(code OAuthCode) (string, error) {
	value, err := RandomToken(32)
	if err != nil {
		return "", fmt.Errorf("generate authorization code: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	code.ExpiresAt = time.Now().Add(5 * time.Minute)
	s.codes[value] = code
	return value, nil
}

func (s *OAuthStore) Consume(code string) (OAuthCode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.codes[code]
	if !ok || time.Now().After(value.ExpiresAt) {
		delete(s.codes, code)
		return OAuthCode{}, false
	}
	delete(s.codes, code)
	return value, true
}

func (s *OAuthStore) IssueRefreshToken(clientID, resource string, ttl time.Duration) (string, error) {
	clientID = strings.TrimSpace(clientID)
	resource = strings.TrimSpace(resource)
	if clientID == "" {
		return "", errors.New("refresh token client ID is required")
	}
	if resource == "" {
		return "", errors.New("refresh token resource is required")
	}
	if ttl <= 0 {
		return "", errors.New("refresh token TTL must be positive")
	}
	raw, err := RandomToken(48)
	if err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}
	now := time.Now()
	entry := OAuthRefreshToken{
		ClientID: clientID, Resource: resource,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix(),
	}
	digest := refreshTokenDigest(raw)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredRefreshTokensLocked(now.Unix())
	if len(s.refreshTokens) >= maxRefreshTokens {
		return "", fmt.Errorf("OAuth refresh token limit %d reached", maxRefreshTokens)
	}
	s.refreshTokens[digest] = entry
	if err := s.persistRefreshTokensLocked(); err != nil {
		delete(s.refreshTokens, digest)
		return "", err
	}
	return raw, nil
}

func (s *OAuthStore) RotateRefreshToken(raw, clientID, requestedResource string, ttl time.Duration) (string, string, bool, error) {
	raw = strings.TrimSpace(raw)
	clientID = strings.TrimSpace(clientID)
	requestedResource = strings.TrimSpace(requestedResource)
	if raw == "" || clientID == "" || ttl <= 0 {
		return "", "", false, nil
	}
	newRaw, err := RandomToken(48)
	if err != nil {
		return "", "", false, fmt.Errorf("generate rotated refresh token: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneExpiredRefreshTokensLocked(now.Unix())
	oldDigest := refreshTokenDigest(raw)
	entry, ok := s.refreshTokens[oldDigest]
	if !ok || entry.ClientID != clientID {
		return "", "", false, nil
	}
	if requestedResource != "" && requestedResource != entry.Resource {
		return "", "", false, nil
	}
	newEntry := OAuthRefreshToken{
		ClientID: entry.ClientID, Resource: entry.Resource,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix(),
	}
	newDigest := refreshTokenDigest(newRaw)
	delete(s.refreshTokens, oldDigest)
	s.refreshTokens[newDigest] = newEntry
	if err := s.persistRefreshTokensLocked(); err != nil {
		delete(s.refreshTokens, newDigest)
		s.refreshTokens[oldDigest] = entry
		return "", "", false, err
	}
	return newRaw, entry.Resource, true, nil
}

func (s *OAuthStore) pruneExpiredRefreshTokensLocked(now int64) {
	for digest, token := range s.refreshTokens {
		if token.ExpiresAt <= now {
			delete(s.refreshTokens, digest)
		}
	}
}

func (s *OAuthStore) persistRefreshTokensLocked() error {
	if s.refreshPath == "" {
		return nil
	}
	state := refreshTokenState{Version: refreshStateVersion, Tokens: s.refreshTokens}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode OAuth refresh token state: %w", err)
	}
	if len(data) > maxRefreshStateSize {
		return fmt.Errorf("OAuth refresh token state exceeds %d bytes", maxRefreshStateSize)
	}
	if err := atomicfile.Write(s.refreshPath, data, 0o600); err != nil {
		return fmt.Errorf("persist OAuth refresh token state: %w", err)
	}
	return nil
}

func validStoredRefreshToken(digest string, token OAuthRefreshToken, now int64) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(digest)
	return err == nil && len(decoded) == sha256.Size &&
		token.ClientID != "" && token.Resource != "" &&
		token.IssuedAt > 0 && token.IssuedAt <= now+60 &&
		token.ExpiresAt > now && token.ExpiresAt > token.IssuedAt
}

func refreshTokenDigest(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func VerifyPKCE(verifier, challenge string) bool {
	if !validPKCEVerifier(verifier) || !ValidPKCEChallenge(challenge) {
		return false
	}
	digest := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(digest[:])
	return hmac.Equal([]byte(expected), []byte(challenge))
}

func ValidPKCEChallenge(challenge string) bool {
	if len(challenge) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(challenge)
	return err == nil && len(decoded) == sha256.Size && base64.RawURLEncoding.EncodeToString(decoded) == challenge
}

func validPKCEVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, char := range verifier {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		switch char {
		case '-', '.', '_', '~':
		default:
			return false
		}
	}
	return true
}

func RandomToken(n int) (string, error) {
	if n <= 0 {
		return "", errors.New("token byte length must be positive")
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read cryptographic randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func IssueClientID(redirectURIs, grantTypes []string, key string) (string, error) {
	if key == "" {
		return "", errors.New("client registration signing key is required")
	}
	if len(redirectURIs) == 0 {
		return "", errors.New("at least one redirect URI is required")
	}
	if len(grantTypes) == 0 {
		return "", errors.New("at least one grant type is required")
	}
	nonce, err := RandomToken(16)
	if err != nil {
		return "", fmt.Errorf("generate client registration nonce: %w", err)
	}
	registration := clientRegistration{
		Version:      2,
		RedirectURIs: uniqueNonEmptyStrings(redirectURIs),
		GrantTypes:   uniqueNonEmptyStrings(grantTypes),
		IssuedAt:     time.Now().Unix(),
		Nonce:        nonce,
	}
	if len(registration.RedirectURIs) == 0 || len(registration.GrantTypes) == 0 {
		return "", errors.New("client registration contains empty metadata")
	}
	body, err := json.Marshal(registration)
	if err != nil {
		return "", fmt.Errorf("encode client registration: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	payload := dynamicClientPrefix + encoded
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(payload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + signature, nil
}

func ValidateClientID(clientID, key string) bool {
	_, ok := parseClientRegistration(clientID, key)
	return ok
}

func ValidateClientRedirect(clientID, redirectURI, key string) bool {
	registration, ok := parseClientRegistration(clientID, key)
	if !ok || redirectURI == "" {
		return false
	}
	for _, registered := range registration.RedirectURIs {
		if registered == redirectURI {
			return true
		}
	}
	return false
}

func ClientAllowsGrant(clientID, grantType, key string) bool {
	registration, ok := parseClientRegistration(clientID, key)
	if !ok || grantType == "" {
		return false
	}
	for _, registered := range registration.GrantTypes {
		if registered == grantType {
			return true
		}
	}
	return false
}

func parseClientRegistration(clientID, key string) (clientRegistration, bool) {
	if key == "" || !strings.HasPrefix(clientID, dynamicClientPrefix) {
		return clientRegistration{}, false
	}
	parts := strings.Split(strings.TrimPrefix(clientID, dynamicClientPrefix), ".")
	if len(parts) != 2 {
		return clientRegistration{}, false
	}
	payload := dynamicClientPrefix + parts[0]
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return clientRegistration{}, false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return clientRegistration{}, false
	}
	var registration clientRegistration
	if err := json.Unmarshal(data, &registration); err != nil {
		return clientRegistration{}, false
	}
	now := time.Now().Unix()
	if registration.Version != 2 || registration.IssuedAt <= 0 || registration.IssuedAt > now+60 ||
		len(registration.RedirectURIs) == 0 || len(registration.GrantTypes) == 0 {
		return clientRegistration{}, false
	}
	return registration, true
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	clean := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		clean = append(clean, value)
	}
	return clean
}

func IssueToken(issuer, audience, key string, ttl time.Duration) (string, error) {
	if key == "" {
		return "", errors.New("token signing key is required")
	}
	if issuer == "" {
		return "", errors.New("token issuer is required")
	}
	if audience == "" {
		return "", errors.New("token audience is required")
	}
	if ttl <= 0 {
		return "", errors.New("token TTL must be positive")
	}
	now := time.Now()
	claims := tokenClaims{
		Issuer:    issuer,
		Audience:  audience,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode token claims: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

func ValidateToken(token, issuer, audience, key string) bool {
	if key == "" {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	var claims tokenClaims
	if err := json.Unmarshal(data, &claims); err != nil {
		return false
	}
	now := time.Now().Unix()
	return claims.Audience == audience &&
		claims.Issuer == issuer &&
		claims.IssuedAt > 0 &&
		claims.IssuedAt <= now+60 &&
		claims.ExpiresAt > now &&
		claims.ExpiresAt > claims.IssuedAt
}

func ConfiguredLoginValue() string {
	return os.Getenv("AGENTDOCK_OAUTH_" + "PASSWORD")
}

func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func AppendQuery(raw string, values url.Values) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for key, list := range values {
		for _, value := range list {
			q.Add(key, value)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
