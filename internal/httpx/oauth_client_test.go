package httpx

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/uvwt/agentdock/internal/auth"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestCIMDValidatorAcceptsChatGPTMetadataAndCaches(t *testing.T) {
	const clientID = "https://chatgpt.com/oauth/agentdock/client.json"
	const redirectURI = "https://chatgpt.com/connector/oauth/callback-id"
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if request.URL.String() != clientID {
			t.Fatalf("metadata URL = %q", request.URL.String())
		}
		if request.Header.Get("Accept") != "application/json" {
			t.Fatalf("Accept = %q", request.Header.Get("Accept"))
		}
		body := `{
			"redirect_uris":["https://chatgpt.com/connector/oauth/callback-id"],
			"token_endpoint_auth_methods_supported":["none","private_key_jwt"],
			"grant_types":["authorization_code","refresh_token"],
			"response_types":["code"]
		}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
	validator := newOAuthClientValidatorWithHTTPClient(auth.NewOAuthStore(), "legacy-key", client)
	ctx := context.Background()

	if !validator.ValidateClientID(ctx, clientID) {
		t.Fatal("CIMD client ID was rejected")
	}
	if !validator.ValidateRedirect(ctx, clientID, redirectURI) {
		t.Fatal("CIMD redirect URI was rejected")
	}
	if validator.ValidateRedirect(ctx, clientID, "https://chatgpt.com/connector/oauth/other") {
		t.Fatal("unlisted CIMD redirect URI was accepted")
	}
	if !validator.AllowsGrant(ctx, clientID, "authorization_code") ||
		!validator.AllowsGrant(ctx, clientID, "refresh_token") {
		t.Fatal("CIMD grant type was rejected")
	}
	if calls.Load() != 1 {
		t.Fatalf("metadata fetch count = %d, want 1", calls.Load())
	}
}

func TestCIMDValidatorRejectsNonChatGPTClientIDs(t *testing.T) {
	for _, clientID := range []string{
		"",
		"http://chatgpt.com/oauth/test/client.json",
		"https://example.com/oauth/test/client.json",
		"https://chatgpt.com.example/oauth/test/client.json",
		"https://chatgpt.com/oauth/test/client.json?variant=1",
		"https://chatgpt.com/oauth/test/client.json#fragment",
		"https://chatgpt.com/not-oauth/test/client.json",
		"https://chatgpt.com/oauth/test/client.txt",
	} {
		t.Run(clientID, func(t *testing.T) {
			if err := validateChatGPTCIMDURL(clientID); err == nil {
				t.Fatalf("accepted client ID %q", clientID)
			}
		})
	}
}

func TestCIMDValidatorRejectsInvalidMetadata(t *testing.T) {
	const clientID = "https://chatgpt.com/oauth/agentdock/client.json"
	cases := []struct {
		name string
		body string
	}{
		{name: "missing redirect", body: `{"token_endpoint_auth_methods_supported":["none"]}`},
		{name: "invalid redirect", body: `{"redirect_uris":["http://example.com/callback"],"token_endpoint_auth_methods_supported":["none"]}`},
		{name: "private key only", body: `{"redirect_uris":["https://chatgpt.com/connector/oauth/callback"],"token_endpoint_auth_methods_supported":["private_key_jwt"]}`},
		{name: "client credentials", body: `{"redirect_uris":["https://chatgpt.com/connector/oauth/callback"],"token_endpoint_auth_methods_supported":["none"],"grant_types":["client_credentials"]}`},
		{name: "implicit response", body: `{"redirect_uris":["https://chatgpt.com/connector/oauth/callback"],"token_endpoint_auth_methods_supported":["none"],"response_types":["token"]}`},
		{name: "malformed JSON", body: `{`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(test.body)),
					Request:    request,
				}, nil
			})}
			validator := newOAuthClientValidatorWithHTTPClient(auth.NewOAuthStore(), "legacy-key", client)
			if validator.ValidateClientID(context.Background(), clientID) {
				t.Fatal("invalid CIMD metadata was accepted")
			}
		})
	}
}

func TestCIMDValidatorRejectsOversizedDocument(t *testing.T) {
	const clientID = "https://chatgpt.com/oauth/agentdock/client.json"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", cimdDocumentMaxBytes+1))),
			Request:    request,
		}, nil
	})}
	validator := newOAuthClientValidatorWithHTTPClient(auth.NewOAuthStore(), "legacy-key", client)
	if validator.ValidateClientID(context.Background(), clientID) {
		t.Fatal("oversized CIMD metadata was accepted")
	}
}
