package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	// A modernc.org/sqlite ":memory:" database is per-connection; pin the
	// pool to one connection so every query sees the migrated schema and a
	// leaked transaction fails fast rather than silently hitting a blank DB.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyMigrations(db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return db
}

// insertPersonForTest commits a person via the same Tx-only path core
// uses, so the store tests exercise the real insert helper.
func insertPersonForTest(t *testing.T, db *sql.DB, name, by string) int64 {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	id, err := InsertPerson(ctx, tx, name, by)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert person: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id
}

func TestInsertAndGetPerson(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	id := insertPersonForTest(t, db, "Jane Doe", "steward@x.onmicrosoft.com")
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	p, err := GetPerson(ctx, db, id)
	if err != nil {
		t.Fatalf("get person: %v", err)
	}
	if p.Name != "Jane Doe" {
		t.Errorf("name: got %q want %q", p.Name, "Jane Doe")
	}
	if p.CreatedBy != "steward@x.onmicrosoft.com" || p.UpdatedBy != "steward@x.onmicrosoft.com" {
		t.Errorf("created/updated by: got (%q, %q)", p.CreatedBy, p.UpdatedBy)
	}
	if p.CreatedAt == "" || p.UpdatedAt == "" {
		t.Errorf("timestamps not set: created=%q updated=%q", p.CreatedAt, p.UpdatedAt)
	}
}

func TestGetPerson_NotFound(t *testing.T) {
	db := openTestDB(t)
	if _, err := GetPerson(context.Background(), db, 9999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("got %v, want sql.ErrNoRows", err)
	}
}

func TestUpdatePersonName(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := insertPersonForTest(t, db, "Jane Doe", "a@x")

	before, _ := GetPerson(ctx, db, id)

	tx, _ := db.BeginTx(ctx, nil)
	n, err := UpdatePersonName(ctx, tx, id, "Jane Smith", "b@x")
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("update: %v", err)
	}
	if n != 1 {
		t.Errorf("rows affected: got %d want 1", n)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	after, _ := GetPerson(ctx, db, id)
	if after.Name != "Jane Smith" {
		t.Errorf("name not updated: %q", after.Name)
	}
	if after.UpdatedBy != "b@x" {
		t.Errorf("updated_by: got %q want %q", after.UpdatedBy, "b@x")
	}
	if after.CreatedAt != before.CreatedAt {
		t.Errorf("created_at changed: %q -> %q", before.CreatedAt, after.CreatedAt)
	}
}

func TestUpdatePersonName_Missing(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	tx, _ := db.BeginTx(ctx, nil)
	defer tx.Rollback()
	n, err := UpdatePersonName(ctx, tx, 12345, "Nobody", "a@x")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if n != 0 {
		t.Errorf("rows affected for missing person: got %d want 0", n)
	}
}

func TestListPersons(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	insertPersonForTest(t, db, "Bob Brown", "a@x")
	insertPersonForTest(t, db, "alice adams", "a@x")
	insertPersonForTest(t, db, "Carol Clark", "a@x")

	all, err := ListPersons(ctx, db, "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("list all: got %d want 3", len(all))
	}
	// Case-insensitive name order: alice, Bob, Carol.
	if all[0].Name != "alice adams" || all[1].Name != "Bob Brown" || all[2].Name != "Carol Clark" {
		t.Errorf("unexpected order: %q, %q, %q", all[0].Name, all[1].Name, all[2].Name)
	}

	hits, err := ListPersons(ctx, db, "al")
	if err != nil {
		t.Fatalf("list search: %v", err)
	}
	// "al" matches "alice adams" and "Carol Clark" (Cl... no — substring 'al' in 'Carol'? c-a-r-o-l, no 'al'). Only alice.
	if len(hits) != 1 || hits[0].Name != "alice adams" {
		t.Errorf("search 'al': got %d hits %+v", len(hits), names(hits))
	}
}

func TestListPersons_EscapesWildcards(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	insertPersonForTest(t, db, "100% Cotton", "a@x")
	insertPersonForTest(t, db, "Plain Name", "a@x")

	// A literal "%" must not behave as a wildcard matching everything.
	hits, err := ListPersons(ctx, db, "%")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(hits) != 1 || hits[0].Name != "100% Cotton" {
		t.Errorf("literal %% search: got %d %+v", len(hits), names(hits))
	}
}

func names(ps []Person) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}
