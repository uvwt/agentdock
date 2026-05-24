package auth

import (
	"net/http"
	"strings"
)

type Bearer struct {
	Token string
}

func (b Bearer) Enabled() bool {
	return b.Token != ""
}

func (b Bearer) Authorized(r *http.Request) bool {
	if !b.Enabled() {
		return true
	}
	return strings.TrimSpace(r.Header.Get("Authorization")) == "Bearer "+b.Token
}
