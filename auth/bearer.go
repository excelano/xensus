package auth

import (
	"net/http"
	"strings"
)

// extractBearer returns the JWT from an "Authorization: Bearer <jwt>"
// header, or the empty string if absent or malformed. It is the only
// shape of Bearer credential Xensus accepts.
func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}
