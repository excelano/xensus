package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

func insertPendingForTest(t *testing.T, db *sql.DB, upn, by string) int64 {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	id, err := InsertPendingSteward(ctx, tx, upn, by)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert pending: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id
}

func TestInsertAndGetPendingByUPN_CaseInsensitive(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := insertPendingForTest(t, db, "Bob@Contoso.com", "alice@x")

	p, err := GetPendingStewardByUPN(ctx, db, "bob@contoso.com")
	if err != nil {
		t.Fatalf("get pending: %v", err)
	}
	if p.ID != id || p.UserUPN != "Bob@Contoso.com" || p.InvitedBy != "alice@x" {
		t.Errorf("pending fields: %+v", p)
	}
}

func TestGetPendingStewardByUPN_NotFound(t *testing.T) {
	db := openTestDB(t)
	if _, err := GetPendingStewardByUPN(context.Background(), db, "nobody@x"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("got %v, want sql.ErrNoRows", err)
	}
}

func TestGetPendingStewardByID(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := insertPendingForTest(t, db, "bob@x", "alice@x")

	p, err := GetPendingStewardByID(ctx, db, id)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if p.UserUPN != "bob@x" {
		t.Errorf("upn: got %q", p.UserUPN)
	}
	if _, err := GetPendingStewardByID(ctx, db, 9999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("missing id: got %v, want sql.ErrNoRows", err)
	}
}

func TestListPendingStewards_Order(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	insertPendingForTest(t, db, "carol@x", "alice@x")
	insertPendingForTest(t, db, "bob@x", "alice@x")

	pending, err := ListPendingStewards(ctx, db)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("got %d pending, want 2", len(pending))
	}
	if pending[0].UserUPN != "bob@x" || pending[1].UserUPN != "carol@x" {
		t.Errorf("order: %q, %q", pending[0].UserUPN, pending[1].UserUPN)
	}
}

func TestDeletePendingSteward(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := insertPendingForTest(t, db, "bob@x", "alice@x")

	tx, _ := db.BeginTx(ctx, nil)
	n, err := DeletePendingSteward(ctx, tx, id)
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

	if _, err := GetPendingStewardByUPN(ctx, db, "bob@x"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("pending still present after delete: %v", err)
	}

	tx2, _ := db.BeginTx(ctx, nil)
	defer tx2.Rollback()
	n2, err := DeletePendingSteward(ctx, tx2, id)
	if err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second delete affected %d rows, want 0", n2)
	}
}

func TestInsertPendingSteward_RejectsDuplicateUPN(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	insertPendingForTest(t, db, "bob@x", "alice@x")

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()
	if _, err := InsertPendingSteward(ctx, tx, "bob@x", "alice@x"); err == nil {
		t.Error("expected duplicate UPN to violate the UNIQUE constraint")
	}
}
