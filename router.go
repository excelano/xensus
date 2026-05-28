package main

import (
	"database/sql"
	"net/http"

	"github.com/excelano/xensus/api"
	"github.com/excelano/xensus/auth"
)

// registerRoutes wires the top-level HTTP routes. If authr is nil
// (because the deployment isn't bound yet or OIDC env vars are unset)
// the auth and /me routes are skipped — /health stays reachable so
// operators can still probe the process.
func registerRoutes(mux *http.ServeMux, db *sql.DB, authr *auth.Authenticator) {
	mux.HandleFunc("GET /health", healthHandler(db))

	if authr == nil {
		return
	}
	authr.Register(mux)
	mux.Handle("GET /api/v1/me", authr.Authenticate(http.HandlerFunc(api.Me)))
}
