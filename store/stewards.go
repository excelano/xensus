package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Steward mirrors a row of the stewards table. A steward is identified by
// the M365 object ID (stable across renames); UserUPN is the human-readable
// principal captured at promotion time. RemovedAt is empty for an active
// steward and holds the timestamp once demoted — demotion is soft so the
// audit trail and a future re-promotion stay coherent.
type Steward struct {
	ID         int64
	UserOID    string
	UserUPN    string
	PromotedAt string
	PromotedBy string
	RemovedAt  string
	RemovedBy  string
}

// Removed reports whether the steward has been demoted.
func (s Steward) Removed() bool { return s.RemovedAt != "" }

const stewardColumns = `id, user_oid, user_upn, promoted_at, promoted_by, removed_at, removed_by`

func scanSteward(row interface{ Scan(...any) error }) (Steward, error) {
	var (
		s                    Steward
		removedAt, removedBy sql.NullString
	)
	err := row.Scan(&s.ID, &s.UserOID, &s.UserUPN, &s.PromotedAt, &s.PromotedBy, &removedAt, &removedBy)
	s.RemovedAt = removedAt.String
	s.RemovedBy = removedBy.String
	return s, err
}

// IsActiveSteward reports whether the given M365 object ID currently holds
// an active steward row (removed_at IS NULL). It takes a rowQueryer so the
// same check serves a standalone read (auth middleware, *sql.DB) and a read
// inside a write transaction (core's claim path, *sql.Tx).
func IsActiveSteward(ctx context.Context, q rowQueryer, userOID string) (bool, error) {
	var count int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM stewards
		WHERE user_oid = ? AND removed_at IS NULL
	`, userOID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query active steward: %w", err)
	}
	return count > 0, nil
}

// IsActiveStewardByUPN reports whether an active steward exists whose UPN
// matches (case-insensitively). It backs the promote-time duplicate guard:
// a UPN that already belongs to a steward shouldn't be re-invited. UPNs are
// matched case-insensitively because the casing in an ID token isn't
// guaranteed stable.
func IsActiveStewardByUPN(ctx context.Context, q rowQueryer, upn string) (bool, error) {
	var count int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM stewards
		WHERE lower(user_upn) = lower(?) AND removed_at IS NULL
	`, upn).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("query active steward by upn: %w", err)
	}
	return count > 0, nil
}

// GetSteward reads a single steward by ID regardless of removed state, so a
// demote can inspect the row (and reject an already-removed one) without a
// separate query. Returns sql.ErrNoRows when no such steward exists; core
// maps that to ErrNotFound.
func GetSteward(ctx context.Context, q rowQueryer, id int64) (Steward, error) {
	row := q.QueryRowContext(ctx, `SELECT `+stewardColumns+` FROM stewards WHERE id = ?`, id)
	return scanSteward(row)
}

// ListActiveStewards returns current stewards (removed_at IS NULL) ordered by
// UPN (case-insensitive). Demoted stewards are excluded — they live on only
// in the audit log and as soft-removed rows.
func ListActiveStewards(ctx context.Context, db *sql.DB) ([]Steward, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+stewardColumns+` FROM stewards WHERE removed_at IS NULL ORDER BY user_upn COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("list active stewards: %w", err)
	}
	defer rows.Close()

	var out []Steward
	for rows.Next() {
		s, err := scanSteward(rows)
		if err != nil {
			return nil, fmt.Errorf("scan steward: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stewards: %w", err)
	}
	return out, nil
}

// InsertSteward creates an active steward row and returns its assigned ID.
// The caller's transaction is responsible for the matching audit row. The
// partial unique index on (user_oid) WHERE removed_at IS NULL guarantees one
// active row per identity; a previously demoted steward re-promotes cleanly
// as a new row because the old one carries removed_at.
func InsertSteward(ctx context.Context, tx *sql.Tx, userOID, userUPN, promotedBy string) (int64, error) {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO stewards (user_oid, user_upn, promoted_by) VALUES (?, ?, ?)`,
		userOID, userUPN, promotedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("insert steward: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("steward last insert id: %w", err)
	}
	return id, nil
}

// DemoteSteward soft-removes an active steward by stamping removed_at/
// removed_by. It only touches a row that isn't already removed, so a demote
// aimed at an already-demoted steward affects zero rows — core reads that as
// "gone" and returns ErrNotFound. The rows-affected count lets the caller
// make that call without a second read.
func DemoteSteward(ctx context.Context, tx *sql.Tx, id int64, by string) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE stewards
		SET removed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), removed_by = ?
		WHERE id = ? AND removed_at IS NULL
	`, by, id)
	if err != nil {
		return 0, fmt.Errorf("demote steward: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("steward rows affected: %w", err)
	}
	return n, nil
}
