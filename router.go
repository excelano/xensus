package main

import (
	"database/sql"
	"net/http"

	"github.com/excelano/xensus/api"
	"github.com/excelano/xensus/auth"
)

// registerRoutes wires the top-level HTTP routes. If authr is nil
// (because the deployment isn't bound yet or OIDC env vars are unset)
// the auth and API routes are skipped — /health stays reachable so
// operators can still probe the process.
//
// Read routes are gated with RequireUser (any signed-in tenant member);
// mutating routes with RequireSteward. Reads stay open to non-stewards
// by design — the registry is meant to be widely consultable.
func registerRoutes(mux *http.ServeMux, db *sql.DB, authr *auth.Authenticator) {
	mux.HandleFunc("GET /health", healthHandler(db))

	if authr == nil {
		return
	}
	authr.Register(mux)

	apiH := api.New(db)
	user := func(h http.HandlerFunc) http.Handler {
		return authr.Authenticate(auth.RequireUser(h))
	}
	steward := func(h http.HandlerFunc) http.Handler {
		return authr.Authenticate(auth.RequireSteward(h))
	}

	mux.Handle("GET /api/v1/me", authr.Authenticate(http.HandlerFunc(api.Me)))

	mux.Handle("GET /api/v1/persons", user(apiH.ListPersons))
	mux.Handle("GET /api/v1/persons.csv", user(apiH.ExportCSV))
	mux.Handle("GET /api/v1/persons/{id}", user(apiH.GetPerson))
	mux.Handle("POST /api/v1/persons", steward(apiH.CreatePerson))
	mux.Handle("PATCH /api/v1/persons/{id}", steward(apiH.RenamePerson))
}
