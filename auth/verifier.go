// Package auth handles OIDC sign-in for the web UI, Bearer JWT
// validation for the REST API, and a unified User context value shared
// between both paths. See /home/anderix/.claude/plans/lucky-crunching-dawn.md
// (Slice 3a) for the design decisions captured here.
package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Claims is Xensus's view of an ID token after verification. The field
// names are Microsoft Entra-specific (oid, tid, preferred_username) —
// Xensus is Microsoft-only by design, so we don't pretend to be a
// general OIDC client.
type Claims struct {
	OID   string
	UPN   string
	TID   string
	Nonce string
}

// Verifier verifies a raw ID token JWT against the configured issuer,
// audience, signing keys, and expiry. The real implementation wraps
// coreos/go-oidc; tests substitute a fake to exercise downstream logic
// (notably the tenant-mismatch rejection) without standing up an OIDC
// provider.
type Verifier interface {
	Verify(ctx context.Context, rawIDToken string) (Claims, error)
}

type oidcVerifier struct {
	inner *oidc.IDTokenVerifier
}

func (v *oidcVerifier) Verify(ctx context.Context, raw string) (Claims, error) {
	tok, err := v.inner.Verify(ctx, raw)
	if err != nil {
		return Claims{}, fmt.Errorf("verify id token: %w", err)
	}
	var c struct {
		OID string `json:"oid"`
		UPN string `json:"preferred_username"`
		TID string `json:"tid"`
	}
	if err := tok.Claims(&c); err != nil {
		return Claims{}, fmt.Errorf("decode id token claims: %w", err)
	}
	if c.OID == "" {
		return Claims{}, fmt.Errorf("id token missing 'oid' claim — only Microsoft Entra tokens are supported")
	}
	if c.TID == "" {
		return Claims{}, fmt.Errorf("id token missing 'tid' claim")
	}
	return Claims{OID: c.OID, UPN: c.UPN, TID: c.TID, Nonce: tok.Nonce}, nil
}
