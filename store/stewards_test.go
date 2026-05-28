package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

// insertStewardForTest commits an active steward via the Tx-only path core
// uses and returns its id.
func insertStewardForTest(t *testing.T, db *sql.DB, oid, upn, by string) int64 {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	id, err := InsertSteward(ctx, tx, oid, upn, by)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert steward: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id
}

func demoteStewardForTest(t *testing.T, db *sql.DB, id int64, by string) int64 {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	n, err := DemoteSteward(ctx, tx, id, by)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("demote steward: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return n
}

func TestInsertAndGetSteward(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := insertStewardForTest(t, db, "oid-1", "alice@x", "bootstrap")

	s, err := GetSteward(ctx, db, id)
	if err != nil {
		t.Fatalf("get steward: %v", err)
	}
	if s.UserOID != "oid-1" || s.UserUPN != "alice@x" || s.PromotedBy != "bootstrap" {
		t.Errorf("steward fields: %+v", s)
	}
	if s.Removed() {
		t.Error("freshly inserted steward reads as removed")
	}
}

func TestGetSteward_NotFound(t *testing.T) {
	db := openTestDB(t)
	if _, err := GetSteward(context.Background(), db, 9999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("got %v, want sql.ErrNoRows", err)
	}
}

func TestIsActiveSteward(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	insertStewardForTest(t, db, "oid-1", "alice@x", "bootstrap")

	got, err := IsActiveSteward(ctx, db, "oid-1")
	if err != nil {
		t.Fatalf("is active: %v", err)
	}
	if !got {
		t.Error("expected oid-1 to be an active steward")
	}
	got, err = IsActiveSteward(ctx, db, "oid-unknown")
	if err != nil {
		t.Fatalf("is active: %v", err)
	}
	if got {
		t.Error("unknown oid reported as active steward")
	}
}

func TestIsActiveStewardByUPN_CaseInsensitive(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	insertStewardForTest(t, db, "oid-1", "Alice@Contoso.com", "bootstrap")

	got, err := IsActiveStewardByUPN(ctx, db, "alice@contoso.com")
	if err != nil {
		t.Fatalf("by upn: %v", err)
	}
	if !got {
		t.Error("expected case-insensitive UPN match")
	}
	got, err = IsActiveStewardByUPN(ctx, db, "bob@contoso.com")
	if err != nil {
		t.Fatalf("by upn: %v", err)
	}
	if got {
		t.Error("non-steward UPN matched")
	}
}

func TestListActiveStewards_OrderAndExcludesRemoved(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	insertStewardForTest(t, db, "oid-c", "carol@x", "bootstrap")
	insertStewardForTest(t, db, "oid-a", "alice@x", "bootstrap")
	demoted := insertStewardForTest(t, db, "oid-b", "bob@x", "bootstrap")
	demoteStewardForTest(t, db, demoted, "alice@x")

	stewards, err := ListActiveStewards(ctx, db)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(stewards) != 2 {
		t.Fatalf("got %d active stewards, want 2", len(stewards))
	}
	if stewards[0].UserUPN != "alice@x" || stewards[1].UserUPN != "carol@x" {
		t.Errorf("order/contents: %q, %q", stewards[0].UserUPN, stewards[1].UserUPN)
	}
}

func TestDemoteSteward_SoftAndAllowsRepromote(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := insertStewardForTest(t, db, "oid-1", "alice@x", "bootstrap")

	if n := demoteStewardForTest(t, db, id, "boss@x"); n != 1 {
		t.Errorf("first demote affected %d rows, want 1", n)
	}
	// Second demote is a no-op.
	if n := demoteStewardForTest(t, db, id, "boss@x"); n != 0 {
		t.Errorf("second demote affected %d rows, want 0", n)
	}

	active, err := IsActiveSteward(ctx, db, "oid-1")
	if err != nil {
		t.Fatalf("is active: %v", err)
	}
	if active {
		t.Error("demoted steward still reads as active")
	}

	// The partial unique index permits re-promoting the same OID as a fresh
	// active row, since the old row carries removed_at.
	newID := insertStewardForTest(t, db, "oid-1", "alice@x", "boss@x")
	if newID == id {
		t.Error("re-promote reused the demoted row id")
	}
	active, err = IsActiveSteward(ctx, db, "oid-1")
	if err != nil {
		t.Fatalf("is active: %v", err)
	}
	if !active {
		t.Error("re-promoted steward not active")
	}
}

func TestInsertSteward_RejectsDuplicateActiveOID(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	insertStewardForTest(t, db, "oid-1", "alice@x", "bootstrap")

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()
	if _, err := InsertSteward(ctx, tx, "oid-1", "alice@x", "boss@x"); err == nil {
		t.Error("expected duplicate active OID to violate the partial unique index")
	}
}
