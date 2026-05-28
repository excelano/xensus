package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

// insertAssociationForTest commits an association via the same Tx-only path
// core uses.
func insertAssociationForTest(t *testing.T, db *sql.DB, personID, systemID int64, foreignID, by string) int64 {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	id, err := InsertAssociation(ctx, tx, personID, systemID, foreignID, by)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert association: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id
}

func TestInsertAndGetAssociation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	pid := insertPersonForTest(t, db, "Jane Doe", "a@x")
	sid := insertSystemForTest(t, db, "Workday", "a@x")

	aid := insertAssociationForTest(t, db, pid, sid, "EMP-12345", "a@x")
	if aid <= 0 {
		t.Fatalf("expected positive id, got %d", aid)
	}

	a, err := GetAssociation(ctx, db, aid)
	if err != nil {
		t.Fatalf("get association: %v", err)
	}
	if a.PersonID != pid || a.SystemID != sid {
		t.Errorf("ids: got person %d system %d, want %d %d", a.PersonID, a.SystemID, pid, sid)
	}
	if a.SystemName != "Workday" {
		t.Errorf("system name not joined: %q", a.SystemName)
	}
	if a.ForeignID != "EMP-12345" {
		t.Errorf("foreign id: got %q", a.ForeignID)
	}
	if a.CreatedBy != "a@x" {
		t.Errorf("created by: got %q", a.CreatedBy)
	}
}

func TestGetAssociation_NotFound(t *testing.T) {
	db := openTestDB(t)
	if _, err := GetAssociation(context.Background(), db, 9999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("got %v, want sql.ErrNoRows", err)
	}
}

func TestInsertAssociation_AllowsEmptyForeignID(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	pid := insertPersonForTest(t, db, "Jane Doe", "a@x")
	sid := insertSystemForTest(t, db, "Workday", "a@x")

	aid := insertAssociationForTest(t, db, pid, sid, "", "a@x")
	a, err := GetAssociation(ctx, db, aid)
	if err != nil {
		t.Fatalf("get association: %v", err)
	}
	if a.ForeignID != "" {
		t.Errorf("foreign id: got %q, want empty", a.ForeignID)
	}
}

func TestDeleteAssociation_HardDeletes(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	pid := insertPersonForTest(t, db, "Jane Doe", "a@x")
	sid := insertSystemForTest(t, db, "Workday", "a@x")
	aid := insertAssociationForTest(t, db, pid, sid, "EMP-1", "a@x")

	tx, _ := db.BeginTx(ctx, nil)
	n, err := DeleteAssociation(ctx, tx, aid)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("delete: %v", err)
	}
	if n != 1 {
		t.Errorf("first delete affected %d rows, want 1", n)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// The row is gone for good.
	if _, err := GetAssociation(ctx, db, aid); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("after delete, get returned %v, want sql.ErrNoRows", err)
	}

	// A second delete is a no-op.
	tx2, _ := db.BeginTx(ctx, nil)
	defer tx2.Rollback()
	n2, err := DeleteAssociation(ctx, tx2, aid)
	if err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second delete affected %d rows, want 0", n2)
	}
}

func TestListAssociationsForPerson_OrderAndDuplicates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	jane := insertPersonForTest(t, db, "Jane Doe", "a@x")
	other := insertPersonForTest(t, db, "John Roe", "a@x")
	workday := insertSystemForTest(t, db, "Workday", "a@x")
	fieldglass := insertSystemForTest(t, db, "FieldGlass", "a@x")

	// Jane links to Workday twice (duplicates are allowed) and FieldGlass once.
	w1 := insertAssociationForTest(t, db, jane, workday, "W-1", "a@x")
	insertAssociationForTest(t, db, jane, fieldglass, "F-1", "a@x")
	w2 := insertAssociationForTest(t, db, jane, workday, "W-2", "a@x")
	// Another person's link must not bleed into Jane's list.
	insertAssociationForTest(t, db, other, workday, "W-OTHER", "a@x")

	links, err := ListAssociationsForPerson(ctx, db, jane)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(links) != 3 {
		t.Fatalf("got %d links, want 3", len(links))
	}
	// Ordered by system name (FieldGlass before Workday), then association id.
	if links[0].SystemName != "FieldGlass" {
		t.Errorf("first link system: got %q, want FieldGlass", links[0].SystemName)
	}
	if links[1].ID != w1 || links[2].ID != w2 {
		t.Errorf("duplicate Workday order: got ids %d,%d want %d,%d",
			links[1].ID, links[2].ID, w1, w2)
	}
}

func TestListAssociationsForPerson_Empty(t *testing.T) {
	db := openTestDB(t)
	pid := insertPersonForTest(t, db, "Jane Doe", "a@x")
	links, err := ListAssociationsForPerson(context.Background(), db, pid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("got %d links, want 0", len(links))
	}
}
