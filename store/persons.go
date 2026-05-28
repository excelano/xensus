package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Person mirrors a row of the persons table. Timestamps are RFC3339 UTC
// strings exactly as SQLite stores them; created_by / updated_by hold the
// acting steward's UPN for at-a-glance reading (the authoritative actor
// trail, with stable OIDs, lives in audit_log).
type Person struct {
	ID        int64
	Name      string
	CreatedAt string
	CreatedBy string
	UpdatedAt string
	UpdatedBy string
}

// rowQueryer is satisfied by both *sql.DB and *sql.Tx. GetPerson takes it
// so the same reader serves standalone reads (api list/detail) and reads
// inside a write transaction (core captures a person's prior name to put
// the before/after diff in the rename audit row).
type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const personColumns = `id, name, created_at, created_by, updated_at, updated_by`

func scanPerson(row interface{ Scan(...any) error }) (Person, error) {
	var p Person
	err := row.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.CreatedBy, &p.UpdatedAt, &p.UpdatedBy)
	return p, err
}

// InsertPerson creates a new person and returns its assigned ID. The
// caller's transaction is responsible for the matching audit row. name
// may be empty — the registry hands out IDs first and names can follow.
func InsertPerson(ctx context.Context, tx *sql.Tx, name, by string) (int64, error) {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO persons (name, created_by, updated_by) VALUES (?, ?, ?)`,
		name, by, by,
	)
	if err != nil {
		return 0, fmt.Errorf("insert person: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("person last insert id: %w", err)
	}
	return id, nil
}

// UpdatePersonName sets a person's name and bumps updated_at/updated_by.
// It returns the number of rows affected so the caller can distinguish a
// successful update (1) from a missing person (0) without a prior read.
func UpdatePersonName(ctx context.Context, tx *sql.Tx, id int64, name, by string) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE persons
		SET name = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), updated_by = ?
		WHERE id = ?
	`, name, by, id)
	if err != nil {
		return 0, fmt.Errorf("update person name: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("person rows affected: %w", err)
	}
	return n, nil
}

// GetPerson reads a single person by ID. It returns sql.ErrNoRows when no
// such person exists; core maps that to its ErrNotFound sentinel.
func GetPerson(ctx context.Context, q rowQueryer, id int64) (Person, error) {
	row := q.QueryRowContext(ctx, `SELECT `+personColumns+` FROM persons WHERE id = ?`, id)
	p, err := scanPerson(row)
	if err != nil {
		return Person{}, err
	}
	return p, nil
}

// ListPersons returns persons ordered by name (case-insensitive), then ID.
// A non-empty query filters by case-insensitive substring match on name;
// LIKE wildcards in the query are escaped so a literal "%" search works.
func ListPersons(ctx context.Context, db *sql.DB, query string) ([]Person, error) {
	var (
		rows *sql.Rows
		err  error
	)
	base := `SELECT ` + personColumns + ` FROM persons`
	order := ` ORDER BY name COLLATE NOCASE, id`
	if q := strings.TrimSpace(query); q != "" {
		rows, err = db.QueryContext(ctx,
			base+` WHERE name LIKE '%' || ? || '%' ESCAPE '\'`+order,
			escapeLike(q),
		)
	} else {
		rows, err = db.QueryContext(ctx, base+order)
	}
	if err != nil {
		return nil, fmt.Errorf("list persons: %w", err)
	}
	defer rows.Close()

	var out []Person
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, fmt.Errorf("scan person: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate persons: %w", err)
	}
	return out, nil
}

// escapeLike neutralizes the LIKE metacharacters so a user's search term
// is matched literally. Paired with ESCAPE '\' in the query.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
