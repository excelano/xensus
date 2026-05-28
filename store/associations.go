package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Association links a person to a system, carrying that person's identifier
// in the foreign system (foreign_id). SystemName is joined in from the
// systems table for display — the join is always satisfiable because
// system_id is a foreign key into a table from which rows are never
// deleted. Associations themselves are hard-deleted: there is no removed_at
// column, and the permanent trail lives in audit_log.
type Association struct {
	ID         int64
	PersonID   int64
	SystemID   int64
	SystemName string
	ForeignID  string
	CreatedAt  string
	CreatedBy  string
}

// associationColumns selects an association joined to its system's name.
// The join is an inner join: system_id references systems(id), and systems
// are never deleted (only disabled), so every association has a system.
const associationColumns = `
	a.id, a.person_id, a.system_id, s.name, a.foreign_id, a.created_at, a.created_by
	FROM associations a JOIN systems s ON s.id = a.system_id`

func scanAssociation(row interface{ Scan(...any) error }) (Association, error) {
	var a Association
	err := row.Scan(&a.ID, &a.PersonID, &a.SystemID, &a.SystemName, &a.ForeignID, &a.CreatedAt, &a.CreatedBy)
	return a, err
}

// InsertAssociation creates a person↔system link and returns its ID. The
// caller's transaction is responsible for the matching audit row. Duplicate
// (person, system) pairs are allowed by design, so this never checks for an
// existing link. foreign_id may be empty — the registry can record that a
// person shows up in a system before their ID there is known.
func InsertAssociation(ctx context.Context, tx *sql.Tx, personID, systemID int64, foreignID, by string) (int64, error) {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO associations (person_id, system_id, foreign_id, created_by) VALUES (?, ?, ?, ?)`,
		personID, systemID, foreignID, by,
	)
	if err != nil {
		return 0, fmt.Errorf("insert association: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("association last insert id: %w", err)
	}
	return id, nil
}

// GetAssociation reads a single association by ID, with its system name
// joined in. It returns sql.ErrNoRows when no such association exists; core
// maps that to ErrNotFound. core reads the row before a delete so it can put
// the system name and foreign_id in the audit trail.
func GetAssociation(ctx context.Context, q rowQueryer, id int64) (Association, error) {
	row := q.QueryRowContext(ctx, `SELECT `+associationColumns+` WHERE a.id = ?`, id)
	a, err := scanAssociation(row)
	if err != nil {
		return Association{}, err
	}
	return a, nil
}

// DeleteAssociation hard-deletes an association and returns the number of
// rows removed, so the caller can distinguish a real delete (1) from a
// missing association (0) without a prior read. The row is gone for good;
// its trace survives only in audit_log.
func DeleteAssociation(ctx context.Context, tx *sql.Tx, id int64) (int64, error) {
	res, err := tx.ExecContext(ctx, `DELETE FROM associations WHERE id = ?`, id)
	if err != nil {
		return 0, fmt.Errorf("delete association: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("association rows affected: %w", err)
	}
	return n, nil
}

// ListAssociationsForPerson returns a person's current links, ordered by
// system name (case-insensitive) then association ID. Duplicates (same
// system listed twice) are allowed and both appear.
func ListAssociationsForPerson(ctx context.Context, db *sql.DB, personID int64) ([]Association, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+associationColumns+` WHERE a.person_id = ? ORDER BY s.name COLLATE NOCASE, a.id`,
		personID,
	)
	if err != nil {
		return nil, fmt.Errorf("list associations for person %d: %w", personID, err)
	}
	defer rows.Close()

	var out []Association
	for rows.Next() {
		a, err := scanAssociation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan association: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate associations: %w", err)
	}
	return out, nil
}
