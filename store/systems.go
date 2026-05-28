package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// System mirrors a row of the systems table. Like Person, timestamps are
// RFC3339 UTC strings and created_by/updated_by hold the acting steward's
// UPN. Unlike persons, a system can be disabled: disabling drops it from
// the active set but keeps the row and its full history for the permanent
// record. DisabledAt is empty for an active system and holds the timestamp
// once disabled; the nullable columns are scanned through sql.NullString so
// an active row reads as the empty string. Disabling is reversible — a
// steward can enable a system again, which clears these fields.
type System struct {
	ID         int64
	Name       string
	CreatedAt  string
	CreatedBy  string
	UpdatedAt  string
	UpdatedBy  string
	DisabledAt string
	DisabledBy string
}

// Disabled reports whether the system is currently disabled.
func (s System) Disabled() bool { return s.DisabledAt != "" }

const systemColumns = `id, name, created_at, created_by, updated_at, updated_by, disabled_at, disabled_by`

func scanSystem(row interface{ Scan(...any) error }) (System, error) {
	var (
		s                      System
		disabledAt, disabledBy sql.NullString
	)
	err := row.Scan(&s.ID, &s.Name, &s.CreatedAt, &s.CreatedBy, &s.UpdatedAt, &s.UpdatedBy, &disabledAt, &disabledBy)
	s.DisabledAt = disabledAt.String
	s.DisabledBy = disabledBy.String
	return s, err
}

// InsertSystem creates a new system and returns its assigned ID. The
// caller's transaction is responsible for the matching audit row. A
// system's name is its identity, so callers (core) reject a blank name
// before reaching here.
func InsertSystem(ctx context.Context, tx *sql.Tx, name, by string) (int64, error) {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO systems (name, created_by, updated_by) VALUES (?, ?, ?)`,
		name, by, by,
	)
	if err != nil {
		return 0, fmt.Errorf("insert system: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("system last insert id: %w", err)
	}
	return id, nil
}

// UpdateSystemName renames an active system and bumps updated_at/updated_by.
// It only touches a row that isn't disabled, so a rename aimed at a disabled
// system affects zero rows — core reads that as "gone" and returns
// ErrNotFound. The rows-affected count lets the caller make that call
// without a prior read.
func UpdateSystemName(ctx context.Context, tx *sql.Tx, id int64, name, by string) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE systems
		SET name = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), updated_by = ?
		WHERE id = ? AND disabled_at IS NULL
	`, name, by, id)
	if err != nil {
		return 0, fmt.Errorf("update system name: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("system rows affected: %w", err)
	}
	return n, nil
}

// DisableSystem disables an active system by stamping disabled_at/
// disabled_by. It's a no-op against an already-disabled row (zero rows
// affected), so core can keep disabling idempotent without a second audit
// row.
func DisableSystem(ctx context.Context, tx *sql.Tx, id int64, by string) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE systems
		SET disabled_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), disabled_by = ?
		WHERE id = ? AND disabled_at IS NULL
	`, by, id)
	if err != nil {
		return 0, fmt.Errorf("disable system: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("system rows affected: %w", err)
	}
	return n, nil
}

// EnableSystem re-enables a disabled system by clearing disabled_at/
// disabled_by. It's a no-op against an already-active row (zero rows
// affected), keeping enabling idempotent. The acting steward is captured in
// the audit row core writes, so this needs no "by" argument.
func EnableSystem(ctx context.Context, tx *sql.Tx, id int64) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE systems
		SET disabled_at = NULL, disabled_by = NULL
		WHERE id = ? AND disabled_at IS NOT NULL
	`, id)
	if err != nil {
		return 0, fmt.Errorf("enable system: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("system rows affected: %w", err)
	}
	return n, nil
}

// GetSystem reads a single system by ID regardless of disabled state, so a
// disabled system's detail page and history stay reachable. It returns
// sql.ErrNoRows when no such system exists; core maps that to ErrNotFound.
func GetSystem(ctx context.Context, q rowQueryer, id int64) (System, error) {
	row := q.QueryRowContext(ctx, `SELECT `+systemColumns+` FROM systems WHERE id = ?`, id)
	s, err := scanSystem(row)
	if err != nil {
		return System{}, err
	}
	return s, nil
}

// ListSystems returns active (not disabled) systems ordered by name
// (case-insensitive), then ID. A non-empty query filters by
// case-insensitive substring match on name, with LIKE wildcards escaped so
// a literal "%" search works.
func ListSystems(ctx context.Context, db *sql.DB, query string) ([]System, error) {
	var (
		rows *sql.Rows
		err  error
	)
	base := `SELECT ` + systemColumns + ` FROM systems WHERE disabled_at IS NULL`
	order := ` ORDER BY name COLLATE NOCASE, id`
	if q := strings.TrimSpace(query); q != "" {
		rows, err = db.QueryContext(ctx,
			base+` AND name LIKE '%' || ? || '%' ESCAPE '\'`+order,
			escapeLike(q),
		)
	} else {
		rows, err = db.QueryContext(ctx, base+order)
	}
	if err != nil {
		return nil, fmt.Errorf("list systems: %w", err)
	}
	defer rows.Close()

	return collectSystems(rows)
}

// ListDisabledSystems returns disabled systems, most recently disabled
// first, so a steward reviewing the disabled set sees the latest changes at
// the top.
func ListDisabledSystems(ctx context.Context, db *sql.DB) ([]System, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+systemColumns+` FROM systems WHERE disabled_at IS NOT NULL ORDER BY disabled_at DESC, name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list disabled systems: %w", err)
	}
	defer rows.Close()

	return collectSystems(rows)
}

// CountDisabledSystems returns how many systems are currently disabled, used
// to surface a "View N disabled" link on the active list.
func CountDisabledSystems(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM systems WHERE disabled_at IS NOT NULL`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count disabled systems: %w", err)
	}
	return n, nil
}

func collectSystems(rows *sql.Rows) ([]System, error) {
	var out []System
	for rows.Next() {
		s, err := scanSystem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan system: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate systems: %w", err)
	}
	return out, nil
}
