package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// BoundTenantID returns the tenant ID this deployment is bound to, or
// the empty string if no tenant has been bound yet (i.e. the config row
// has tenant_id IS NULL). Reads use *sql.DB per the package contract.
func BoundTenantID(ctx context.Context, db *sql.DB) (string, error) {
	var tid sql.NullString
	err := db.QueryRowContext(ctx, `SELECT tenant_id FROM config WHERE id = 1`).Scan(&tid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("config singleton row missing — migrations did not run")
	}
	if err != nil {
		return "", fmt.Errorf("read tenant_id: %w", err)
	}
	if !tid.Valid {
		return "", nil
	}
	return tid.String, nil
}
