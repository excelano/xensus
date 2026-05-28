package web

import (
	"net/http"
	"net/url"

	"github.com/excelano/xensus/auth"
)

// Register wires the HTML page routes onto mux. Every route runs through
// authr.Authenticate first (which populates request context) and then a
// web-flavored gate: requireUser redirects anonymous visitors to the
// sign-in page rather than returning a bare 401, and requireSteward 403s
// a signed-in non-steward who reaches a write route.
func (h *Handlers) Register(mux *http.ServeMux, authr *auth.Authenticator) {
	user := func(fn http.HandlerFunc) http.Handler {
		return authr.Authenticate(requireUser(fn))
	}
	steward := func(fn http.HandlerFunc) http.Handler {
		return authr.Authenticate(requireSteward(fn))
	}

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/persons", http.StatusFound)
	})
	mux.Handle("GET /persons", user(h.ListPersons))
	mux.Handle("POST /persons", steward(h.CreatePerson))
	mux.Handle("GET /persons/{id}", user(h.PersonDetail))
	mux.Handle("POST /persons/{id}", steward(h.RenamePerson))

	mux.Handle("GET /systems", user(h.ListSystems))
	mux.Handle("POST /systems", steward(h.CreateSystem))
	mux.Handle("GET /systems/disabled", user(h.ListDisabledSystems))
	mux.Handle("GET /systems/{id}", user(h.SystemDetail))
	mux.Handle("POST /systems/{id}", steward(h.RenameSystem))
	mux.Handle("POST /systems/{id}/disable", steward(h.DisableSystem))
	mux.Handle("POST /systems/{id}/enable", steward(h.EnableSystem))
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
