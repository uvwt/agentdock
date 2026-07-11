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
)

type OAuthStore struct {
	mu    sync.Mutex
	codes map[string]OAuthCode
}

type OAuthCode struct {
	ClientID    string
	RedirectURI string
	Challenge   string
	State       string
	ExpiresAt   time.Time
}

type tokenClaims struct {
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func NewOAuthStore() *OAuthStore {
	return &OAuthStore{codes: map[string]OAuthCode{}}
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

func IssueToken(issuer, key string, ttl time.Duration) (string, error) {
	if key == "" {
		return "", errors.New("token signing key is required")
	}
	if issuer == "" {
		return "", errors.New("token issuer is required")
	}
	if ttl <= 0 {
		return "", errors.New("token ttl must be positive")
	}
	now := time.Now()
	claims := tokenClaims{
		Issuer:    issuer,
		Audience:  issuer,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode token claims: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(encoded)) // hash.Hash.Write 对有效内存写入不会返回错误。
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

func ValidateToken(token, issuer, key string) bool {
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
	return claims.Audience == issuer &&
		claims.Issuer == issuer &&
		claims.IssuedAt > 0 &&
		claims.IssuedAt <= now+60 &&
		claims.ExpiresAt > now &&
		claims.ExpiresAt > claims.IssuedAt
}

func ConfiguredLoginValue() string {
	return os.Getenv("AGENTDOCK_OAUTH_" + "PASSWORD")
}

func ConfiguredClientSecret() string {
	return os.Getenv("AGENTDOCK_OAUTH_CLIENT_" + "SECRET")
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
