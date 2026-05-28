package store

import (
	"context"
	"database/sql"
	"fmt"
)

// PendingSteward mirrors a row of the pending_stewards table: a UPN that a
// steward has invited but who hasn't yet signed in to claim the role. The
// row is keyed by UPN (not OID) because the invitee's object ID isn't known
// until their first sign-in — claiming the invite is what produces the
// active steward row with a real OID.
type PendingSteward struct {
	ID        int64
	UserUPN   string
	InvitedAt string
	InvitedBy string
}

const pendingStewardColumns = `id, user_upn, invited_at, invited_by`

func scanPendingSteward(row interface{ Scan(...any) error }) (PendingSteward, error) {
	var p PendingSteward
	err := row.Scan(&p.ID, &p.UserUPN, &p.InvitedAt, &p.InvitedBy)
	return p, err
}

// ListPendingStewards returns outstanding invitations ordered by UPN
// (case-insensitive).
func ListPendingStewards(ctx context.Context, db *sql.DB) ([]PendingSteward, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+pendingStewardColumns+` FROM pending_stewards ORDER BY user_upn COLLATE NOCASE, id`)
	if err != nil {
		return nil, fmt.Errorf("list pending stewards: %w", err)
	}
	defer rows.Close()

	var out []PendingSteward
	for rows.Next() {
		p, err := scanPendingSteward(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pending steward: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending stewards: %w", err)
	}
	return out, nil
}

// GetPendingStewardByUPN finds an invitation by UPN (case-insensitive). It
// backs both the promote-time duplicate guard and the sign-in claim. Returns
// sql.ErrNoRows when no invitation matches.
func GetPendingStewardByUPN(ctx context.Context, q rowQueryer, upn string) (PendingSteward, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+pendingStewardColumns+` FROM pending_stewards WHERE lower(user_upn) = lower(?)`, upn)
	return scanPendingSteward(row)
}

// GetPendingStewardByID finds an invitation by its row ID. It backs the
// cancel-invitation path, which carries the id from the listing. Returns
// sql.ErrNoRows when no invitation matches.
func GetPendingStewardByID(ctx context.Context, q rowQueryer, id int64) (PendingSteward, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+pendingStewardColumns+` FROM pending_stewards WHERE id = ?`, id)
	return scanPendingSteward(row)
}

// InsertPendingSteward records an invitation and returns its assigned ID. The
// user_upn UNIQUE constraint backstops the core-level duplicate guard against
// concurrent invites of the same UPN. The caller's transaction is responsible
// for the matching audit row.
func InsertPendingSteward(ctx context.Context, tx *sql.Tx, upn, invitedBy string) (int64, error) {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO pending_stewards (user_upn, invited_by) VALUES (?, ?)`,
		upn, invitedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("insert pending steward: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("pending steward last insert id: %w", err)
	}
	return id, nil
}

// DeletePendingSteward consumes an invitation by ID, returning rows affected.
// Claiming an invite deletes the pending row in the same transaction that
// creates the active steward, so the invitation can't be claimed twice.
func DeletePendingSteward(ctx context.Context, tx *sql.Tx, id int64) (int64, error) {
	res, err := tx.ExecContext(ctx, `DELETE FROM pending_stewards WHERE id = ?`, id)
	if err != nil {
		return 0, fmt.Errorf("delete pending steward: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("pending steward rows affected: %w", err)
	}
	return n, nil
}
