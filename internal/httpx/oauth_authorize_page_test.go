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
	writeAuthorizeForm(response, values, "")
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

func TestAuthorizationFormCSPAllowsRegisteredRedirectOrigin(t *testing.T) {
	values := url.Values{
		"redirect_uri": {"https://client.example/oauth/callback?source=test"},
	}
	response := httptest.NewRecorder()
	writeAuthorizeForm(response, values, "")

	want := "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' https://client.example; base-uri 'none'; frame-ancestors 'none'"
	if got := response.Header().Get("Content-Security-Policy"); got != want {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, want)
	}
}

func TestAuthorizationFormCSPRejectsInvalidRedirectOrigin(t *testing.T) {
	values := url.Values{"redirect_uri": {"javascript:alert(1)"}}
	response := httptest.NewRecorder()
	writeAuthorizeForm(response, values, "")

	want := "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	if got := response.Header().Get("Content-Security-Policy"); got != want {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, want)
	}
}
