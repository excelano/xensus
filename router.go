package main

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/excelano/xensus/api"
	"github.com/excelano/xensus/auth"
	"github.com/excelano/xensus/config"
	"github.com/excelano/xensus/static"
	"github.com/excelano/xensus/web"
)

// registerRoutes wires the top-level HTTP routes. If authr is nil
// (because the deployment isn't bound yet or OIDC env vars are unset)
// the auth and API routes are skipped — /health stays reachable so
// operators can still probe the process.
//
// Read routes are gated with RequireUser (any signed-in tenant member);
// mutating routes with RequireSteward. Reads stay open to non-stewards
// by design — the registry is meant to be widely consultable.
//
// It returns an error if a handler set fails to initialize (e.g. the web
// templates don't parse) — that's a build-time fault that should stop the
// server from coming up rather than 500 on first page load.
func registerRoutes(mux *http.ServeMux, db *sql.DB, authr *auth.Authenticator, access config.Access) error {
	mux.HandleFunc("GET /health", healthHandler(db))
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static.FS)))

	if authr == nil {
		return nil
	}
	authr.Register(mux)

	apiH := api.New(db)
	user := func(h http.HandlerFunc) http.Handler {
		return authr.Authenticate(auth.RequireUser(h))
	}
	steward := func(h http.HandlerFunc) http.Handler {
		return authr.Authenticate(auth.RequireSteward(h))
	}
	// read picks a surface's read gate: steward-only when the operator
	// locked it via XENSUS_STEWARD_ONLY, otherwise open to any signed-in
	// user (the default). Writes always use steward.
	read := func(surface string) func(http.HandlerFunc) http.Handler {
		if access.StewardOnly(surface) {
			return steward
		}
		return user
	}
	persons := read(config.SurfacePersons)
	systems := read(config.SurfaceSystems)
	stewards := read(config.SurfaceStewards)
	audit := read(config.SurfaceAudit)

	// /me reports on the caller themselves, so it is always user-level —
	// it isn't a registry surface and the access policy doesn't touch it.
	mux.Handle("GET /api/v1/me", authr.Authenticate(http.HandlerFunc(api.Me)))

	mux.Handle("GET /api/v1/persons", persons(apiH.ListPersons))
	mux.Handle("GET /api/v1/persons.csv", persons(apiH.ExportCSV))
	mux.Handle("GET /api/v1/persons/{id}", persons(apiH.GetPerson))
	mux.Handle("POST /api/v1/persons", steward(apiH.CreatePerson))
	mux.Handle("PATCH /api/v1/persons/{id}", steward(apiH.RenamePerson))

	mux.Handle("GET /api/v1/persons/{id}/associations", persons(apiH.ListAssociations))
	mux.Handle("POST /api/v1/persons/{id}/associations", steward(apiH.CreateAssociation))
	mux.Handle("DELETE /api/v1/persons/{id}/associations/{aid}", steward(apiH.RemoveAssociation))

	mux.Handle("GET /api/v1/systems", systems(apiH.ListSystems))
	mux.Handle("GET /api/v1/systems.csv", systems(apiH.ExportSystemsCSV))
	mux.Handle("GET /api/v1/systems/disabled", systems(apiH.ListDisabledSystems))
	mux.Handle("GET /api/v1/systems/{id}", systems(apiH.GetSystem))
	mux.Handle("POST /api/v1/systems", steward(apiH.CreateSystem))
	mux.Handle("PATCH /api/v1/systems/{id}", steward(apiH.RenameSystem))
	mux.Handle("POST /api/v1/systems/{id}/disable", steward(apiH.DisableSystem))
	mux.Handle("POST /api/v1/systems/{id}/enable", steward(apiH.EnableSystem))

	mux.Handle("GET /api/v1/audit", audit(apiH.ListAudit))
	mux.Handle("GET /api/v1/audit.csv", audit(apiH.ExportAuditCSV))

	mux.Handle("GET /api/v1/stewards", stewards(apiH.ListStewards))
	mux.Handle("POST /api/v1/stewards", steward(apiH.PromoteSteward))
	mux.Handle("DELETE /api/v1/stewards/{id}", steward(apiH.DemoteSteward))
	mux.Handle("DELETE /api/v1/stewards/pending/{id}", steward(apiH.CancelInvite))

	webH, err := web.New(db)
	if err != nil {
		return fmt.Errorf("init web handlers: %w", err)
	}
	webH.Register(mux, authr, access)
	return nil
}
