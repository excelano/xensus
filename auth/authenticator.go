package auth

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/excelano/xensus/config"
	"github.com/excelano/xensus/store"
)

// Microsoft's "organizations" OIDC endpoint accepts work/school accounts
// from any Entra tenant. Xensus uses it for the lifetime of the process:
// tenant binding is enforced in business logic via the tid claim, not at
// the JWT issuer layer. The discovery doc's iss field contains the
// {tenantid} placeholder, which is why SkipIssuerCheck is required.
const entraOrganizationsIssuer = "https://login.microsoftonline.com/organizations/v2.0"

// Authenticator owns the OIDC provider, verifier, OAuth2 client config,
// session store, and a handle on the database. It is built once at
// startup and survives tenant binding — the bound tenant_id is held
// behind a mutex so the bootstrap path (Slice 3b) can swap it in
// atomically without restarting the process.
type Authenticator struct {
	mu       sync.RWMutex
	tenantID string

	oauth2   *oauth2.Config
	verifier Verifier
	sessions *SessionStore
	db       *sql.DB
}

// New discovers the Microsoft OIDC provider, assembles the OAuth2
// client config, and reads the current tenant binding (if any) from
// the database. An unbound deployment is valid — the first successful
// sign-in will bind it via core.Bootstrap.
func New(ctx context.Context, cfg *config.Config, db *sql.DB) (*Authenticator, error) {
	if err := cfg.RequireOIDC(); err != nil {
		return nil, err
	}

	tid, err := store.BoundTenantID(ctx, db)
	if err != nil {
		return nil, err
	}

	// Microsoft's /organizations discovery doc has its issuer field set
	// to the literal string "https://login.microsoftonline.com/{tenantid}/v2.0"
	// (with the placeholder). InsecureIssuerURLContext tells go-oidc to
	// skip that match at discovery time; the verifier downstream has
	// SkipIssuerCheck and we enforce tenant binding via the tid claim.
	discCtx := oidc.InsecureIssuerURLContext(ctx, entraOrganizationsIssuer)
	provider, err := oidc.NewProvider(discCtx, entraOrganizationsIssuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery (%s): %w", entraOrganizationsIssuer, err)
	}
	inner := provider.Verifier(&oidc.Config{
		ClientID:        cfg.OIDCClientID,
		SkipIssuerCheck: true,
	})

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

// getTenant returns the currently bound tenant ID, or empty string if
// the deployment hasn't been bootstrapped yet.
func (a *Authenticator) getTenant() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.tenantID
}

// setTenant records the bound tenant after a successful bootstrap.
// Safe to call concurrently with getTenant.
func (a *Authenticator) setTenant(tid string) {
	a.mu.Lock()
	a.tenantID = tid
	a.mu.Unlock()
}

// TenantID returns the currently bound tenant ID. Empty means unbound.
func (a *Authenticator) TenantID() string { return a.getTenant() }

// SessionKeyGenerated reports whether the session store fell back to
// an ephemeral key — callers should warn the operator.
func (a *Authenticator) SessionKeyGenerated() bool { return a.sessions.KeyGenerated() }

// Register attaches the auth routes to the given mux.
func (a *Authenticator) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", a.Login)
	mux.HandleFunc("GET /auth/callback", a.Callback)
	mux.HandleFunc("POST /auth/logout", a.Logout)
}
