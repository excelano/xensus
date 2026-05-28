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

func auditCount(t *testing.T, db *sql.DB, action string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM audit_log WHERE action = ?`, action).Scan(&n); err != nil {
		t.Fatalf("count audit %q: %v", action, err)
	}
	return n
}

func TestCreateSystem_WritesSystemAndAudit(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	s, err := core.CreateSystem(ctx, db, testActor, "  Workday  ")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if s.ID == 0 {
		t.Fatal("system id is zero")
	}
	if s.Name != "Workday" {
		t.Errorf("name not trimmed/stored: %q", s.Name)
	}
	if s.Disabled() {
		t.Error("new system reports disabled")
	}

	var entityID int64
	var details sql.NullString
	if err := db.QueryRowContext(ctx,
		`SELECT entity_id, details FROM audit_log WHERE action='system.create'`,
	).Scan(&entityID, &details); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if entityID != s.ID {
		t.Errorf("audit entity_id: got %d want %d", entityID, s.ID)
	}
	if !details.Valid || !strings.Contains(details.String, "Workday") {
		t.Errorf("audit details missing name: %v", details)
	}
}

func TestCreateSystem_RequiresName(t *testing.T) {
	db := openTestDB(t)
	if _, err := core.CreateSystem(context.Background(), db, testActor, "   "); !errors.Is(err, core.ErrNameRequired) {
		t.Errorf("got %v, want ErrNameRequired", err)
	}
}

func TestCreateSystem_RequiresActor(t *testing.T) {
	db := openTestDB(t)
	if _, err := core.CreateSystem(context.Background(), db, core.Actor{}, "Workday"); err == nil {
		t.Error("expected error for empty actor, got nil")
	}
}

func TestRenameSystem_UpdatesAndAudits(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	s, _ := core.CreateSystem(ctx, db, testActor, "Workday")
	renamed, err := core.RenameSystem(ctx, db, testActor, s.ID, "Workday HCM")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "Workday HCM" {
		t.Errorf("name: got %q", renamed.Name)
	}

	var details string
	if err := db.QueryRowContext(ctx,
		`SELECT details FROM audit_log WHERE action='system.rename'`,
	).Scan(&details); err != nil {
		t.Fatalf("read rename audit: %v", err)
	}
	if !strings.Contains(details, "Workday") || !strings.Contains(details, "Workday HCM") {
		t.Errorf("rename audit missing from/to: %q", details)
	}
}

func TestRenameSystem_NoOpWritesNoAudit(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	s, _ := core.CreateSystem(ctx, db, testActor, "Workday")
	if _, err := core.RenameSystem(ctx, db, testActor, s.ID, "  Workday  "); err != nil {
		t.Fatalf("no-op rename: %v", err)
	}
	if n := auditCount(t, db, "system.rename"); n != 0 {
		t.Errorf("no-op rename wrote %d audit rows, want 0", n)
	}
}

func TestRenameSystem_RequiresName(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	s, _ := core.CreateSystem(ctx, db, testActor, "Workday")
	if _, err := core.RenameSystem(ctx, db, testActor, s.ID, ""); !errors.Is(err, core.ErrNameRequired) {
		t.Errorf("got %v, want ErrNameRequired", err)
	}
}

func TestRenameSystem_NotFound(t *testing.T) {
	db := openTestDB(t)
	if _, err := core.RenameSystem(context.Background(), db, testActor, 9999, "X"); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestRenameSystem_DisabledIsNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	s, _ := core.CreateSystem(ctx, db, testActor, "Workday")
	if _, err := core.DisableSystem(ctx, db, testActor, s.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := core.RenameSystem(ctx, db, testActor, s.ID, "Revived"); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("rename of disabled system: got %v, want ErrNotFound", err)
	}
}

func TestDisableSystem_DisablesAndAudits(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	s, _ := core.CreateSystem(ctx, db, testActor, "FieldGlass")
	disabled, err := core.DisableSystem(ctx, db, testActor, s.ID)
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if !disabled.Disabled() || disabled.DisabledBy != testActor.UPN {
		t.Errorf("not marked disabled: %+v", disabled)
	}
	if n := auditCount(t, db, "system.disable"); n != 1 {
		t.Errorf("disable wrote %d audit rows, want 1", n)
	}
}

func TestDisableSystem_IdempotentNoSecondAudit(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	s, _ := core.CreateSystem(ctx, db, testActor, "FieldGlass")
	if _, err := core.DisableSystem(ctx, db, testActor, s.ID); err != nil {
		t.Fatalf("first disable: %v", err)
	}
	again, err := core.DisableSystem(ctx, db, testActor, s.ID)
	if err != nil {
		t.Fatalf("second disable: %v", err)
	}
	if !again.Disabled() {
		t.Error("second disable returned a non-disabled system")
	}
	if n := auditCount(t, db, "system.disable"); n != 1 {
		t.Errorf("after double disable, %d audit rows, want 1", n)
	}
}

func TestDisableSystem_NotFound(t *testing.T) {
	db := openTestDB(t)
	if _, err := core.DisableSystem(context.Background(), db, testActor, 9999); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestEnableSystem_ReactivatesAndAudits(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	s, _ := core.CreateSystem(ctx, db, testActor, "FieldGlass")
	if _, err := core.DisableSystem(ctx, db, testActor, s.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	enabled, err := core.EnableSystem(ctx, db, testActor, s.ID)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if enabled.Disabled() {
		t.Error("system still disabled after enable")
	}
	if n := auditCount(t, db, "system.enable"); n != 1 {
		t.Errorf("enable wrote %d audit rows, want 1", n)
	}
}

func TestEnableSystem_IdempotentOnActive(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	s, _ := core.CreateSystem(ctx, db, testActor, "Workday")
	again, err := core.EnableSystem(ctx, db, testActor, s.ID)
	if err != nil {
		t.Fatalf("enable active: %v", err)
	}
	if again.Disabled() {
		t.Error("active system reported disabled after no-op enable")
	}
	if n := auditCount(t, db, "system.enable"); n != 0 {
		t.Errorf("no-op enable wrote %d audit rows, want 0", n)
	}
}

func TestEnableSystem_NotFound(t *testing.T) {
	db := openTestDB(t)
	if _, err := core.EnableSystem(context.Background(), db, testActor, 9999); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}
