package core_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/excelano/xensus/core"
)

var testActor = core.Actor{OID: "actor-oid", UPN: "steward@x.onmicrosoft.com"}

func TestCreatePerson_WritesPersonAndAudit(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	p, err := core.CreatePerson(ctx, db, testActor, "  Jane Doe  ")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID == 0 {
		t.Fatal("person id is zero")
	}
	if p.Name != "Jane Doe" {
		t.Errorf("name not trimmed/stored: %q", p.Name)
	}
	if p.CreatedBy != testActor.UPN {
		t.Errorf("created_by: got %q want %q", p.CreatedBy, testActor.UPN)
	}

	var action, entityType string
	var entityID int64
	var details sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT action, entity_type, entity_id, details FROM audit_log WHERE entity_type='person'`,
	).Scan(&action, &entityType, &entityID, &details); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if action != "person.create" {
		t.Errorf("action: got %q", action)
	}
	if entityID != p.ID {
		t.Errorf("audit entity_id: got %d want %d", entityID, p.ID)
	}
	if !details.Valid || !strings.Contains(details.String, "Jane Doe") {
		t.Errorf("audit details missing name: %v", details)
	}
}

func TestCreatePerson_AllowsEmptyName(t *testing.T) {
	db := openTestDB(t)
	p, err := core.CreatePerson(context.Background(), db, testActor, "")
	if err != nil {
		t.Fatalf("create with empty name: %v", err)
	}
	if p.Name != "" {
		t.Errorf("expected empty name, got %q", p.Name)
	}
}

func TestRenamePerson_UpdatesAndAudits(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	p, _ := core.CreatePerson(ctx, db, testActor, "Jane Doe")
	renamed, err := core.RenamePerson(ctx, db, testActor, p.ID, "Jane Smith")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "Jane Smith" {
		t.Errorf("name: got %q", renamed.Name)
	}

	var details string
	if err := db.QueryRowContext(ctx,
		`SELECT details FROM audit_log WHERE action='person.rename'`,
	).Scan(&details); err != nil {
		t.Fatalf("read rename audit: %v", err)
	}
	if !strings.Contains(details, "Jane Doe") || !strings.Contains(details, "Jane Smith") {
		t.Errorf("rename audit missing from/to: %q", details)
	}
}

func TestRenamePerson_NoOpWritesNoAudit(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	p, _ := core.CreatePerson(ctx, db, testActor, "Jane Doe")
	// Rename to the same name (with surrounding space that trims away).
	if _, err := core.RenamePerson(ctx, db, testActor, p.ID, "  Jane Doe  "); err != nil {
		t.Fatalf("no-op rename: %v", err)
	}

	var renameRows int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE action='person.rename'`,
	).Scan(&renameRows); err != nil {
		t.Fatal(err)
	}
	if renameRows != 0 {
		t.Errorf("no-op rename wrote %d audit rows, want 0", renameRows)
	}
}

func TestRenamePerson_NotFound(t *testing.T) {
	db := openTestDB(t)
	if _, err := core.RenamePerson(context.Background(), db, testActor, 9999, "X"); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestCreatePerson_RequiresActor(t *testing.T) {
	db := openTestDB(t)
	if _, err := core.CreatePerson(context.Background(), db, core.Actor{}, "X"); err == nil {
		t.Error("expected error for empty actor, got nil")
	}
}
