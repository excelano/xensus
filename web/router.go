package web

import (
	"net/http"
	"net/url"

	"github.com/excelano/xensus/auth"
	"github.com/excelano/xensus/config"
)

// Register wires the HTML page routes onto mux. Every route runs through
// authr.Authenticate first (which populates request context) and then a
// web-flavored gate: requireUser redirects anonymous visitors to the
// sign-in page rather than returning a bare 401, and requireSteward 403s
// a signed-in non-steward who reaches a write route.
func (h *Handlers) Register(mux *http.ServeMux, authr *auth.Authenticator, access config.Access) {
	h.access = access
	user := func(fn http.HandlerFunc) http.Handler {
		return authr.Authenticate(requireUser(fn))
	}
	steward := func(fn http.HandlerFunc) http.Handler {
		return authr.Authenticate(requireSteward(fn))
	}
	// read picks a surface's read gate to mirror the JSON API: steward-only
	// when locked via XENSUS_STEWARD_ONLY, otherwise open to any signed-in
	// user. A locked surface 403s a signed-in non-steward (the web gate's
	// behavior) rather than returning a bare 401.
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

	// The landing route authenticates (without gating) so it can send the
	// visitor to the first surface they may read. With the default open
	// policy that's always /persons; under XENSUS_STEWARD_ONLY a non-steward
	// is routed past a locked surface rather than bounced into a 403.
	mux.Handle("GET /{$}", authr.Authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, landingPath(access, r), http.StatusFound)
	})))
	mux.Handle("GET /persons", persons(h.ListPersons))
	mux.Handle("POST /persons", steward(h.CreatePerson))
	mux.Handle("GET /persons/{id}", persons(h.PersonDetail))
	mux.Handle("POST /persons/{id}", steward(h.RenamePerson))
	mux.Handle("POST /persons/{id}/associations", steward(h.AddAssociation))
	mux.Handle("POST /persons/{id}/associations/{aid}/remove", steward(h.RemoveAssociation))

	mux.Handle("GET /stewards", stewards(h.Stewards))
	mux.Handle("POST /stewards", steward(h.AddSteward))
	mux.Handle("POST /stewards/{id}/remove", steward(h.RemoveSteward))
	mux.Handle("POST /stewards/pending/{id}/cancel", steward(h.CancelInvite))

	mux.Handle("GET /audit", audit(h.Audit))

	mux.Handle("GET /systems", systems(h.ListSystems))
	mux.Handle("POST /systems", steward(h.CreateSystem))
	mux.Handle("GET /systems/disabled", systems(h.ListDisabledSystems))
	mux.Handle("GET /systems/{id}", systems(h.SystemDetail))
	mux.Handle("POST /systems/{id}", steward(h.RenameSystem))
	mux.Handle("POST /systems/{id}/disable", steward(h.DisableSystem))
	mux.Handle("POST /systems/{id}/enable", steward(h.EnableSystem))
}

// landingPath returns the path the root redirect should send a visitor to:
// the first surface they may read. A steward reads everything, so they always
// land on /persons; a non-steward skips any surface locked to stewards. If
// every surface is locked (and the visitor isn't a steward) it falls back to
// /persons, which then enforces the lock.
func landingPath(access config.Access, r *http.Request) string {
	u, _ := auth.UserFrom(r.Context())
	steward := u != nil && u.IsSteward
	for _, s := range []struct{ surface, path string }{
		{config.SurfacePersons, "/persons"},
		{config.SurfaceSystems, "/systems"},
		{config.SurfaceStewards, "/stewards"},
		{config.SurfaceAudit, "/audit"},
	} {
		if steward || !access.StewardOnly(s.surface) {
			return s.path
		}
	}
	return "/persons"
}

// requireUser sends anonymous visitors to sign in, preserving where they
// were headed via ?return_to= so they land back there afterward.
func requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.UserFrom(r.Context()); !ok {
			http.Redirect(w, r, "/auth/login?return_to="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		next(w, r)
	}
}

// requireSteward gates the write routes. An anonymous visitor is sent to
// sign in; a signed-in non-steward gets 403 (the page's forms aren't
// shown to them, so this is a backstop against a hand-crafted POST).
func requireSteward(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFrom(r.Context())
		if !ok {
			http.Redirect(w, r, "/auth/login?return_to="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		if !u.IsSteward {
			http.Error(w, "steward role required", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}
