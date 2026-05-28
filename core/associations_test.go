package core_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/excelano/xensus/core"
	"github.com/excelano/xensus/store"
)

// seedPersonAndSystem creates one person and one system for association tests.
func seedPersonAndSystem(t *testing.T, db *sql.DB) (personID, systemID int64) {
	t.Helper()
	ctx := context.Background()
	p, err := core.CreatePerson(ctx, db, testActor, "Jane Doe")
	if err != nil {
		t.Fatalf("seed person: %v", err)
	}
	s, err := core.CreateSystem(ctx, db, testActor, "Workday")
	if err != nil {
		t.Fatalf("seed system: %v", err)
	}
	return p.ID, s.ID
}

func TestCreateAssociation_WritesAssociationAndAudit(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	pid, sid := seedPersonAndSystem(t, db)

	a, err := core.CreateAssociation(ctx, db, testActor, pid, sid, "  EMP-12345  ")
	if err != nil {
		t.Fatalf("create association: %v", err)
	}
	if a.ID == 0 {
		t.Fatal("association id is zero")
	}
	if a.ForeignID != "EMP-12345" {
		t.Errorf("foreign id not trimmed/stored: %q", a.ForeignID)
	}
	if a.SystemName != "Workday" {
		t.Errorf("system name not joined: %q", a.SystemName)
	}

	// The audit row is filed against the person so it shows on their timeline.
	var (
		entityType string
		entityID   int64
		details    sql.NullString
	)
	if err := db.QueryRowContext(ctx,
		`SELECT entity_type, entity_id, details FROM audit_log WHERE action='association.create'`,
	).Scan(&entityType, &entityID, &details); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if entityType != "person" || entityID != pid {
		t.Errorf("audit scope: got (%q, %d), want (person, %d)", entityType, entityID, pid)
	}
	if !details.Valid || !strings.Contains(details.String, "Workday") || !strings.Contains(details.String, "EMP-12345") {
		t.Errorf("audit details missing system/foreign id: %v", details)
	}
}

func TestCreateAssociation_AllowsEmptyForeignID(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	pid, sid := seedPersonAndSystem(t, db)

	a, err := core.CreateAssociation(ctx, db, testActor, pid, sid, "   ")
	if err != nil {
		t.Fatalf("create association: %v", err)
	}
	if a.ForeignID != "" {
		t.Errorf("foreign id: got %q, want empty", a.ForeignID)
	}
}

func TestCreateAssociation_PersonNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	_, sid := seedPersonAndSystem(t, db)

	if _, err := core.CreateAssociation(ctx, db, testActor, 9999, sid, "x"); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestCreateAssociation_SystemNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	pid, _ := seedPersonAndSystem(t, db)

	if _, err := core.CreateAssociation(ctx, db, testActor, pid, 9999, "x"); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestCreateAssociation_RequiresActor(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	pid, sid := seedPersonAndSystem(t, db)

	if _, err := core.CreateAssociation(ctx, db, core.Actor{}, pid, sid, "x"); err == nil {
		t.Error("expected error for empty actor, got nil")
	}
}

func TestCreateAssociation_AllowsDuplicates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	pid, sid := seedPersonAndSystem(t, db)

	a1, err := core.CreateAssociation(ctx, db, testActor, pid, sid, "EMP-1")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	a2, err := core.CreateAssociation(ctx, db, testActor, pid, sid, "EMP-2")
	if err != nil {
		t.Fatalf("duplicate create: %v", err)
	}
	if a1.ID == a2.ID {
		t.Errorf("duplicate links share id %d", a1.ID)
	}
	if n := auditCount(t, db, "association.create"); n != 2 {
		t.Errorf("two creates wrote %d audit rows, want 2", n)
	}
}

func TestRemoveAssociation_DeletesAndAudits(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	pid, sid := seedPersonAndSystem(t, db)
	a, err := core.CreateAssociation(ctx, db, testActor, pid, sid, "EMP-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := core.RemoveAssociation(ctx, db, testActor, pid, a.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// The join row is gone for good.
	if _, err := store.GetAssociation(ctx, db, a.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("association still present after remove: %v", err)
	}
	if n := auditCount(t, db, "association.remove"); n != 1 {
		t.Errorf("remove wrote %d audit rows, want 1", n)
	}
}

func TestRemoveAssociation_NotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	pid, _ := seedPersonAndSystem(t, db)

	if err := core.RemoveAssociation(ctx, db, testActor, pid, 9999); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestRemoveAssociation_WrongPersonIsNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	pid, sid := seedPersonAndSystem(t, db)
	other, err := core.CreatePerson(ctx, db, testActor, "John Roe")
	if err != nil {
		t.Fatalf("create other person: %v", err)
	}
	a, err := core.CreateAssociation(ctx, db, testActor, pid, sid, "EMP-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Removing Jane's association under John's id must fail and change nothing.
	if err := core.RemoveAssociation(ctx, db, testActor, other.ID, a.ID); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
	if _, err := store.GetAssociation(ctx, db, a.ID); err != nil {
		t.Errorf("association was touched by wrong-person remove: %v", err)
	}
	if n := auditCount(t, db, "association.remove"); n != 0 {
		t.Errorf("wrong-person remove wrote %d audit rows, want 0", n)
	}
}

func TestRemoveAssociation_RequiresActor(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	pid, sid := seedPersonAndSystem(t, db)
	a, err := core.CreateAssociation(ctx, db, testActor, pid, sid, "EMP-1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := core.RemoveAssociation(ctx, db, core.Actor{}, pid, a.ID); err == nil {
		t.Error("expected error for empty actor, got nil")
	}
}
