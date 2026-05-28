package auth

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/excelano/xensus/config"
	"github.com/excelano/xensus/store"
)

// Authenticator owns the bits that need to outlive a single request:
// the OIDC provider + verifier (shared by cookie and Bearer paths), the
// OAuth2 client config, the cookie session store, and a handle on the
// database for steward lookups. One instance per process.
type Authenticator struct {
	tenantID string
	oauth2   *oauth2.Config
	verifier Verifier
	sessions *SessionStore
	db       *sql.DB
}

// New discovers the OIDC provider for the bound tenant and assembles
// the Authenticator. It returns an error if no tenant is bound yet —
// Slice 3b wires the bootstrap path that handles the first sign-in
// against an unbound deployment.
func New(ctx context.Context, cfg *config.Config, db *sql.DB) (*Authenticator, error) {
	tid, err := store.BoundTenantID(ctx, db)
	if err != nil {
		return nil, err
	}
	if tid == "" {
		return nil, fmt.Errorf(
			"tenant not bound — Slice 3b will wire the bootstrap flow; " +
				"for now seed manually: sqlite3 <data-dir>/xensus.sqlite \"UPDATE config SET tenant_id='<your-tenant-id>' WHERE id=1\"")
	}
	if err := cfg.RequireOIDC(); err != nil {
		return nil, err
	}

	issuer := "https://login.microsoftonline.com/" + tid + "/v2.0"
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for tenant %s: %w", tid, err)
	}
	inner := provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID})

	sessions, err := NewSessionStore(cfg.SessionKey, cfg.TrustProxy)
	if err != nil {
		return nil, err
	}

	return &Authenticator{
		tenantID: tid,
		oauth2: &oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			RedirectURL:  cfg.OIDCRedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		verifier: &oidcVerifier{inner: inner},
		sessions: sessions,
		db:       db,
	}, nil
}

// TenantID returns the tenant this Authenticator is bound to; useful
// for tests and for log lines that want to identify the deployment.
func (a *Authenticator) TenantID() string { return a.tenantID }

// SessionKeyGenerated reports whether the session store fell back to
// an ephemeral key — callers should warn the operator.
func (a *Authenticator) SessionKeyGenerated() bool { return a.sessions.KeyGenerated() }

// Register attaches the auth routes (login, callback, logout) to the
// given mux. /me lives in api/ and is attached separately by router.go
// so api keeps owning JSON endpoints.
func (a *Authenticator) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", a.Login)
	mux.HandleFunc("GET /auth/callback", a.Callback)
	mux.HandleFunc("POST /auth/logout", a.Logout)
}
