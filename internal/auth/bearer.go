package auth

import (
	"crypto/subtle"
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
	token, ok := ParseBearerToken(r.Header.Get("Authorization"))
	return ok && subtle.ConstantTimeCompare([]byte(token), []byte(b.Token)) == 1
}

func ParseBearerToken(header string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(header))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || fields[1] == "" {
		return "", false
	}
	return fields[1], true
}
