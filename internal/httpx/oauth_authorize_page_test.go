package httpx

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAuthorizePageEscapesOAuthValues(t *testing.T) {
	values := url.Values{
		"response_type":         {"code"},
		"client_id":             {`client"><script>alert(1)</script>`},
		"redirect_uri":          {`https://client.example/callback?next=<script>alert(1)</script>`},
		"code_challenge":        {"challenge"},
		"code_challenge_method": {"S256"},
		"resource":              {"https://agentdock.example/mcp"},
		"state":                 {`state"><img src=x onerror=alert(1)>`},
	}

	response := httptest.NewRecorder()
	writeAuthorizeForm(response, values, "", "Test Client")
	body := response.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") || strings.Contains(body, "<img src=x") {
		t.Fatalf("authorization page rendered unescaped OAuth values: %s", body)
	}
	for _, escaped := range []string{"&lt;script&gt;alert(1)&lt;/script&gt;", "&lt;img src=x onerror=alert(1)&gt;"} {
		if !strings.Contains(body, escaped) {
			t.Fatalf("authorization page does not contain escaped value %q", escaped)
		}
	}
}

func TestAuthorizePagePostsToCleanAuthorizeEndpoint(t *testing.T) {
	values := url.Values{
		"client_id":    {"client-id"},
		"redirect_uri": {"https://client.example/oauth/callback"},
	}
	response := httptest.NewRecorder()
	writeAuthorizeForm(response, values, "", "Test Client")

	body := response.Body.String()
	if !strings.Contains(body, `<form method="post" action="/oauth/authorize" autocomplete="on">`) {
		t.Fatalf("authorization form does not post to the clean endpoint: %s", body)
	}
	if strings.Contains(body, `action="/oauth/authorize?`) {
		t.Fatalf("authorization form action unexpectedly contains OAuth query parameters: %s", body)
	}
}

func TestAuthorizePageShowsClientIdentityAndRedirectHost(t *testing.T) {
	values := url.Values{"redirect_uri": {"https://client.example:8443/oauth/callback"}, "state": {"request-state"}}
	response := httptest.NewRecorder()
	writeAuthorizeForm(response, values, "", "测试客户端")
	body := response.Body.String()
	for _, want := range []string{"测试客户端", ">测</span>", "验证后返回 client.example:8443", "验证并连接", "拒绝并返回", "error=access_denied", "state=request-state"} {
		if !strings.Contains(body, want) {
			t.Fatalf("authorization page missing %q", want)
		}
	}
}

func TestAuthorizationFormCSPAllowsOnlyRegisteredRedirectOrigin(t *testing.T) {
	values := url.Values{"redirect_uri": {"https://client.example:8443/oauth/callback?source=test"}}
	response := httptest.NewRecorder()
	writeAuthorizeForm(response, values, "", "Test Client")

	want := "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' https://client.example:8443; base-uri 'none'; frame-ancestors 'none'"
	if got := response.Header().Get("Content-Security-Policy"); got != want {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, want)
	}
}

func TestAuthorizationFormCSPFallsBackToSelfForInvalidRedirect(t *testing.T) {
	values := url.Values{"redirect_uri": {"javascript:alert(1)"}}
	response := httptest.NewRecorder()
	writeAuthorizeForm(response, values, "", "Test Client")

	want := "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	if got := response.Header().Get("Content-Security-Policy"); got != want {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, want)
	}
}
