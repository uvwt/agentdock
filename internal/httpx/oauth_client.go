package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/auth"
)

const (
	cimdDocumentMaxBytes = 64 << 10
	cimdCacheTTL         = 5 * time.Minute
)

type oauthClientValidator struct {
	store            *auth.OAuthStore
	legacySigningKey string
	httpClient       *http.Client

	mu    sync.Mutex
	cache map[string]cachedClientMetadata
}

type cachedClientMetadata struct {
	metadata clientMetadataDocument
	expires  time.Time
}

type clientMetadataDocument struct {
	RedirectURIs                      []string `json:"redirect_uris"`
	TokenEndpointAuthMethod           string   `json:"token_endpoint_auth_method"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	GrantTypes                        []string `json:"grant_types"`
	ResponseTypes                     []string `json:"response_types"`
}

func newOAuthClientValidator(store *auth.OAuthStore, legacySigningKey string) *oauthClientValidator {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return newOAuthClientValidatorWithHTTPClient(store, legacySigningKey, client)
}

func newOAuthClientValidatorWithHTTPClient(store *auth.OAuthStore, legacySigningKey string, client *http.Client) *oauthClientValidator {
	if store == nil {
		store = auth.NewOAuthStore()
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &oauthClientValidator{
		store:            store,
		legacySigningKey: legacySigningKey,
		httpClient:       client,
		cache:            make(map[string]cachedClientMetadata),
	}
}

func (v *oauthClientValidator) ValidateClientID(ctx context.Context, clientID string) bool {
	clientID = strings.TrimSpace(clientID)
	if v.store.ValidateClientID(clientID, v.legacySigningKey) {
		return true
	}
	_, err := v.clientMetadata(ctx, clientID)
	return err == nil
}

func (v *oauthClientValidator) ValidateRedirect(ctx context.Context, clientID, redirectURI string) bool {
	clientID = strings.TrimSpace(clientID)
	redirectURI = strings.TrimSpace(redirectURI)
	if v.store.ValidateClientRedirect(clientID, redirectURI, v.legacySigningKey) {
		return true
	}
	metadata, err := v.clientMetadata(ctx, clientID)
	if err != nil {
		return false
	}
	return containsString(metadata.RedirectURIs, redirectURI)
}

func (v *oauthClientValidator) AllowsGrant(ctx context.Context, clientID, grantType string) bool {
	clientID = strings.TrimSpace(clientID)
	grantType = strings.TrimSpace(grantType)
	if v.store.ClientAllowsGrant(clientID, grantType, v.legacySigningKey) {
		return true
	}
	metadata, err := v.clientMetadata(ctx, clientID)
	if err != nil {
		return false
	}
	return containsString(metadata.GrantTypes, grantType)
}

func (v *oauthClientValidator) clientMetadata(ctx context.Context, clientID string) (clientMetadataDocument, error) {
	clientID = strings.TrimSpace(clientID)
	if err := validateChatGPTCIMDURL(clientID); err != nil {
		return clientMetadataDocument{}, err
	}

	now := time.Now()
	v.mu.Lock()
	cached, ok := v.cache[clientID]
	if ok && now.Before(cached.expires) {
		v.mu.Unlock()
		return cached.metadata, nil
	}
	if ok {
		delete(v.cache, clientID)
	}
	v.mu.Unlock()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return clientMetadataDocument{}, fmt.Errorf("create client metadata request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := v.httpClient.Do(request)
	if err != nil {
		return clientMetadataDocument{}, fmt.Errorf("fetch client metadata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return clientMetadataDocument{}, fmt.Errorf("fetch client metadata: status %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, cimdDocumentMaxBytes+1))
	if err != nil {
		return clientMetadataDocument{}, fmt.Errorf("read client metadata: %w", err)
	}
	if len(body) > cimdDocumentMaxBytes {
		return clientMetadataDocument{}, errors.New("client metadata document is too large")
	}
	var metadata clientMetadataDocument
	if err := json.Unmarshal(body, &metadata); err != nil {
		return clientMetadataDocument{}, fmt.Errorf("decode client metadata: %w", err)
	}
	if err := normalizeCIMDMetadata(&metadata); err != nil {
		return clientMetadataDocument{}, err
	}

	v.mu.Lock()
	v.cache[clientID] = cachedClientMetadata{metadata: metadata, expires: now.Add(cimdCacheTTL)}
	v.mu.Unlock()
	return metadata, nil
}

func validateChatGPTCIMDURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" {
		return errors.New("client_id is not an HTTPS client metadata document URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("client metadata document URL must not contain query or fragment")
	}
	if !strings.EqualFold(parsed.Hostname(), "chatgpt.com") || (parsed.Port() != "" && parsed.Port() != "443") {
		return errors.New("client metadata document is not hosted by ChatGPT")
	}
	if !strings.HasPrefix(parsed.EscapedPath(), "/oauth/") || !strings.HasSuffix(parsed.EscapedPath(), "/client.json") {
		return errors.New("client metadata document path is invalid")
	}
	return nil
}

func normalizeCIMDMetadata(metadata *clientMetadataDocument) error {
	if metadata == nil {
		return errors.New("client metadata document is empty")
	}
	redirectURIs := make([]string, 0, len(metadata.RedirectURIs))
	seenRedirects := make(map[string]struct{}, len(metadata.RedirectURIs))
	for _, raw := range metadata.RedirectURIs {
		redirectURI := strings.TrimSpace(raw)
		if !validOAuthRedirectURI(redirectURI) {
			return fmt.Errorf("client metadata contains invalid redirect_uri %q", redirectURI)
		}
		if _, ok := seenRedirects[redirectURI]; ok {
			continue
		}
		seenRedirects[redirectURI] = struct{}{}
		redirectURIs = append(redirectURIs, redirectURI)
	}
	if len(redirectURIs) == 0 || len(redirectURIs) > 10 {
		return errors.New("client metadata redirect_uris must contain between 1 and 10 entries")
	}

	methods := uniqueMetadataValues(metadata.TokenEndpointAuthMethodsSupported)
	if len(methods) == 0 && strings.TrimSpace(metadata.TokenEndpointAuthMethod) != "" {
		methods = []string{strings.TrimSpace(metadata.TokenEndpointAuthMethod)}
	}
	if !containsString(methods, "none") {
		return errors.New("client metadata does not support token_endpoint_auth_method none")
	}

	grantTypes := uniqueMetadataValues(metadata.GrantTypes)
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code"}
	}
	for _, grantType := range grantTypes {
		if grantType != "authorization_code" && grantType != "refresh_token" {
			return fmt.Errorf("client metadata contains unsupported grant_type %q", grantType)
		}
	}
	if !containsString(grantTypes, "authorization_code") {
		return errors.New("client metadata does not support authorization_code")
	}

	responseTypes := uniqueMetadataValues(metadata.ResponseTypes)
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}
	if len(responseTypes) != 1 || responseTypes[0] != "code" {
		return errors.New("client metadata must support only response_type code")
	}

	metadata.RedirectURIs = redirectURIs
	metadata.TokenEndpointAuthMethodsSupported = methods
	metadata.GrantTypes = grantTypes
	metadata.ResponseTypes = responseTypes
	return nil
}

func uniqueMetadataValues(values []string) []string {
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
