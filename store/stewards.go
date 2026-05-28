package store

import (
	"context"
	"database/sql"
	"fmt"
)

// IsActiveSteward reports whether the given M365 object ID currently
// holds an active steward row (removed_at IS NULL). Reads use *sql.DB
// per the package contract; the write side of stewards arrives in
// Slice 7.
func IsActiveSteward(ctx context.Context, db *sql.DB, userOID string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM stewards
		WHERE user_oid = ? AND removed_at IS NULL
	`, userOID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query active steward: %w", err)
	}
	return count > 0, nil
}
