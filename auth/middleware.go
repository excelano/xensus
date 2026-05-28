package auth

import (
	"context"
	"net/http"

	"github.com/excelano/xensus/store"
)

// User is the authenticated principal a handler receives via UserFrom.
// IsSteward is looked up fresh from the database on every authenticated
// request so promote/demote changes take effect immediately rather than
// waiting for the next session cookie issuance.
type User struct {
	OID       string
	UPN       string
	TID       string
	IsSteward bool
}

type contextKey struct{ name string }

var userCtxKey = contextKey{"xensus.user"}

// UserFrom returns the authenticated User from the context, if any. The
// boolean is false when no user has been set — handlers behind
// RequireUser can rely on it being true.
func UserFrom(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(userCtxKey).(*User)
	return u, ok && u != nil
}

// Authenticate tries Bearer first, falling back to the session cookie.
// On success it stuffs a *User into request context. On failure it
// silently continues — callers gate access with RequireUser /
// RequireSteward downstream so anonymous-allowed routes (like /health)
// keep working through the same middleware chain.
func (a *Authenticator) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := a.tryAuthenticate(r)
		if user != nil {
			r = r.WithContext(context.WithValue(r.Context(), userCtxKey, user))
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Authenticator) tryAuthenticate(r *http.Request) *User {
	bound := a.getTenant()
	if bound == "" {
		// No tenant bound yet — no authenticated sessions are possible.
		return nil
	}
	if raw := extractBearer(r); raw != "" {
		claims, err := a.verifier.Verify(r.Context(), raw)
		if err != nil || claims.TID != bound {
			return nil
		}
		return a.userFromClaims(r.Context(), claims)
	}
	sess, err := a.sessions.ReadSession(r)
	if err != nil || sess == nil {
		return nil
	}
	if sess.TID != bound {
		return nil
	}
	isSteward, _ := store.IsActiveSteward(r.Context(), a.db, sess.OID)
	return &User{OID: sess.OID, UPN: sess.UPN, TID: sess.TID, IsSteward: isSteward}
}

func (a *Authenticator) userFromClaims(ctx context.Context, c Claims) *User {
	isSteward, _ := store.IsActiveSteward(ctx, a.db, c.OID)
	return &User{OID: c.OID, UPN: c.UPN, TID: c.TID, IsSteward: isSteward}
}

// RequireUser refuses unauthenticated requests with 401. It is intended
// to wrap the inner handler chain for any route that needs a signed-in
// principal, including API routes that should accept Bearer.
func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFrom(r.Context()); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireSteward gates mutating routes: 401 if unauthenticated, 403 if the
// caller is a signed-in user but not an active steward. Reads stay open to
// any signed-in user (RequireUser); only writes funnel through here.
func RequireSteward(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := UserFrom(r.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !u.IsSteward {
			http.Error(w, "steward role required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
