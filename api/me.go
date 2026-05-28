// Package api owns Xensus's JSON HTTP handlers. Handlers expect that
// the auth middleware has already populated request context with a
// *auth.User; RequireUser (or RequireSteward in later slices) gates
// access at the router level.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/excelano/xensus/auth"
)

// Me returns the calling user's identity and steward bit. It exists so
// that web and API clients have a uniform "who am I" probe; clients
// also use it as a session-validity check.
func Me(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"oid":        u.OID,
		"upn":        u.UPN,
		"tid":        u.TID,
		"is_steward": u.IsSteward,
	})
}
