package core

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// BootstrapClaim is the subset of an OIDC token claim set that Bootstrap
// consumes. It's a plain struct rather than auth.Claims so that core
// stays import-free of the auth package (auth depends on core, not the
// other way around).
type BootstrapClaim struct {
	OID string
	UPN string
	TID string
}

// Bootstrap atomically binds the deployment to a tenant, promotes the
// signing-in user to first steward, and writes a tenant.bootstrap audit
// row — all in a single transaction. Returns the new steward's row ID
// on success or ErrAlreadyBound if a tenant has been bound between the
// caller's check and the transaction (concurrent first sign-ins).
func Bootstrap(ctx context.Context, db *sql.DB, c BootstrapClaim) (stewardID int64, err error) {
	if c.OID == "" || c.UPN == "" || c.TID == "" {
		return 0, fmt.Errorf("bootstrap requires non-empty OID, UPN, TID")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var existing sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT tenant_id FROM config WHERE id=1`).Scan(&existing); err != nil {
		return 0, fmt.Errorf("read tenant_id: %w", err)
	}
	if existing.Valid && existing.String != "" {
		return 0, ErrAlreadyBound
	}

	bootstrappedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx,
		`UPDATE config SET tenant_id=?, bootstrapped_at=? WHERE id=1`,
		c.TID, bootstrappedAt,
	); err != nil {
		return 0, fmt.Errorf("bind tenant: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO stewards (user_oid, user_upn, promoted_by) VALUES (?, ?, 'bootstrap')`,
		c.OID, c.UPN,
	)
	if err != nil {
		return 0, fmt.Errorf("create first steward: %w", err)
	}
	stewardID, err = res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read steward id: %w", err)
	}

	if err = WriteAudit(ctx, tx, AuditEntry{
		Actor:      Actor{OID: c.OID, UPN: c.UPN},
		Action:     "tenant.bootstrap",
		EntityType: "tenant",
		Details: map[string]any{
			"tenant_id":         c.TID,
			"first_steward_id":  stewardID,
			"first_steward_oid": c.OID,
			"first_steward_upn": c.UPN,
		},
	}); err != nil {
		return 0, fmt.Errorf("write bootstrap audit: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit bootstrap: %w", err)
	}
	return stewardID, nil
}
