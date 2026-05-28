package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

// insertSystemForTest commits a system via the same Tx-only path core uses.
func insertSystemForTest(t *testing.T, db *sql.DB, name, by string) int64 {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	id, err := InsertSystem(ctx, tx, name, by)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("insert system: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id
}

// disableSystemForTest disables a system in its own committed Tx.
func disableSystemForTest(t *testing.T, db *sql.DB, id int64, by string) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := DisableSystem(ctx, tx, id, by); err != nil {
		_ = tx.Rollback()
		t.Fatalf("disable system: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestInsertAndGetSystem(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	id := insertSystemForTest(t, db, "Workday", "steward@x.onmicrosoft.com")
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	s, err := GetSystem(ctx, db, id)
	if err != nil {
		t.Fatalf("get system: %v", err)
	}
	if s.Name != "Workday" {
		t.Errorf("name: got %q want %q", s.Name, "Workday")
	}
	if s.CreatedBy != "steward@x.onmicrosoft.com" || s.UpdatedBy != "steward@x.onmicrosoft.com" {
		t.Errorf("created/updated by: got (%q, %q)", s.CreatedBy, s.UpdatedBy)
	}
	if s.Disabled() {
		t.Error("freshly inserted system reports as disabled")
	}
}

func TestGetSystem_NotFound(t *testing.T) {
	db := openTestDB(t)
	if _, err := GetSystem(context.Background(), db, 9999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("got %v, want sql.ErrNoRows", err)
	}
}

func TestUpdateSystemName(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := insertSystemForTest(t, db, "Workday", "a@x")

	tx, _ := db.BeginTx(ctx, nil)
	n, err := UpdateSystemName(ctx, tx, id, "Workday HCM", "b@x")
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

	after, _ := GetSystem(ctx, db, id)
	if after.Name != "Workday HCM" || after.UpdatedBy != "b@x" {
		t.Errorf("after update: %q by %q", after.Name, after.UpdatedBy)
	}
}

func TestUpdateSystemName_DisabledIsUntouched(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := insertSystemForTest(t, db, "Workday", "a@x")
	disableSystemForTest(t, db, id, "a@x")

	tx, _ := db.BeginTx(ctx, nil)
	defer tx.Rollback()
	n, err := UpdateSystemName(ctx, tx, id, "Renamed", "b@x")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if n != 0 {
		t.Errorf("rename of disabled system affected %d rows, want 0", n)
	}
}

func TestDisableSystem_SoftAndIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := insertSystemForTest(t, db, "FieldGlass", "a@x")

	tx, _ := db.BeginTx(ctx, nil)
	n, err := DisableSystem(ctx, tx, id, "a@x")
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("disable: %v", err)
	}
	if n != 1 {
		t.Errorf("first disable affected %d rows, want 1", n)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// The row survives and is marked disabled.
	s, err := GetSystem(ctx, db, id)
	if err != nil {
		t.Fatalf("get after disable: %v", err)
	}
	if !s.Disabled() || s.DisabledBy != "a@x" {
		t.Errorf("not marked disabled: disabled_at=%q disabled_by=%q", s.DisabledAt, s.DisabledBy)
	}

	// A second disable is a no-op.
	tx2, _ := db.BeginTx(ctx, nil)
	defer tx2.Rollback()
	n2, err := DisableSystem(ctx, tx2, id, "a@x")
	if err != nil {
		t.Fatalf("second disable: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second disable affected %d rows, want 0", n2)
	}
}

func TestEnableSystem_ClearsAndIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := insertSystemForTest(t, db, "Workday", "a@x")
	disableSystemForTest(t, db, id, "a@x")

	tx, _ := db.BeginTx(ctx, nil)
	n, err := EnableSystem(ctx, tx, id)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("enable: %v", err)
	}
	if n != 1 {
		t.Errorf("enable affected %d rows, want 1", n)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	s, _ := GetSystem(ctx, db, id)
	if s.Disabled() {
		t.Error("system still disabled after enable")
	}
	if s.DisabledBy != "" {
		t.Errorf("disabled_by not cleared: %q", s.DisabledBy)
	}

	// Enabling an already-active system is a no-op.
	tx2, _ := db.BeginTx(ctx, nil)
	defer tx2.Rollback()
	n2, err := EnableSystem(ctx, tx2, id)
	if err != nil {
		t.Fatalf("second enable: %v", err)
	}
	if n2 != 0 {
		t.Errorf("re-enable affected %d rows, want 0", n2)
	}
}

func TestListSystems_ExcludesDisabledAndSearches(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	insertSystemForTest(t, db, "Workday", "a@x")
	insertSystemForTest(t, db, "fieldglass", "a@x")
	gone := insertSystemForTest(t, db, "Legacy HR", "a@x")
	disableSystemForTest(t, db, gone, "a@x")

	all, err := ListSystems(ctx, db, "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("active list: got %d want 2 (%v)", len(all), systemNames(all))
	}
	// Case-insensitive name order: fieldglass, Workday.
	if all[0].Name != "fieldglass" || all[1].Name != "Workday" {
		t.Errorf("order: %v", systemNames(all))
	}

	hits, err := ListSystems(ctx, db, "field")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].Name != "fieldglass" {
		t.Errorf("search 'field': got %d %v", len(hits), systemNames(hits))
	}
}

func TestListAndCountDisabledSystems(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	insertSystemForTest(t, db, "Workday", "a@x")
	g1 := insertSystemForTest(t, db, "Legacy HR", "a@x")
	g2 := insertSystemForTest(t, db, "Old CRM", "a@x")
	disableSystemForTest(t, db, g1, "a@x")
	disableSystemForTest(t, db, g2, "a@x")

	n, err := CountDisabledSystems(ctx, db)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("disabled count: got %d want 2", n)
	}

	disabled, err := ListDisabledSystems(ctx, db)
	if err != nil {
		t.Fatalf("list disabled: %v", err)
	}
	if len(disabled) != 2 {
		t.Fatalf("disabled list: got %d want 2 (%v)", len(disabled), systemNames(disabled))
	}
	for _, s := range disabled {
		if !s.Disabled() {
			t.Errorf("%q in disabled list but not marked disabled", s.Name)
		}
	}
}

func systemNames(ss []System) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Name
	}
	return out
}
