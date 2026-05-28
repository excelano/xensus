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

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.ApplyMigrations(db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return db
}

func TestBootstrap_BindsTenantAndCreatesSteward(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	claim := core.BootstrapClaim{
		OID: "user-oid-a",
		UPN: "first@a.onmicrosoft.com",
		TID: "tenant-a",
	}
	stewardID, err := core.Bootstrap(ctx, db, claim)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if stewardID == 0 {
		t.Fatalf("steward id is zero")
	}

	var (
		tid            sql.NullString
		bootstrappedAt sql.NullString
	)
	if err := db.QueryRowContext(ctx,
		`SELECT tenant_id, bootstrapped_at FROM config WHERE id=1`,
	).Scan(&tid, &bootstrappedAt); err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !tid.Valid || tid.String != claim.TID {
		t.Errorf("tenant_id: got %v want %q", tid, claim.TID)
	}
	if !bootstrappedAt.Valid || bootstrappedAt.String == "" {
		t.Errorf("bootstrapped_at not set: %v", bootstrappedAt)
	}

	var (
		stewardOID, stewardUPN, promotedBy string
		removedAt                          sql.NullString
	)
	if err := db.QueryRowContext(ctx,
		`SELECT user_oid, user_upn, promoted_by, removed_at FROM stewards WHERE id=?`,
		stewardID,
	).Scan(&stewardOID, &stewardUPN, &promotedBy, &removedAt); err != nil {
		t.Fatalf("read steward: %v", err)
	}
	if stewardOID != claim.OID || stewardUPN != claim.UPN {
		t.Errorf("steward identity: got (%s, %s) want (%s, %s)",
			stewardOID, stewardUPN, claim.OID, claim.UPN)
	}
	if promotedBy != "bootstrap" {
		t.Errorf("promoted_by: got %q want %q", promotedBy, "bootstrap")
	}
	if removedAt.Valid {
		t.Errorf("removed_at unexpectedly set: %v", removedAt)
	}

	var (
		auditAction string
		auditActor  string
		details     sql.NullString
	)
	if err := db.QueryRowContext(ctx,
		`SELECT action, actor_oid, details FROM audit_log WHERE entity_type='tenant'`,
	).Scan(&auditAction, &auditActor, &details); err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	if auditAction != "tenant.bootstrap" {
		t.Errorf("audit action: got %q", auditAction)
	}
	if auditActor != claim.OID {
		t.Errorf("audit actor: got %q want %q", auditActor, claim.OID)
	}
	if !details.Valid || !strings.Contains(details.String, claim.TID) {
		t.Errorf("audit details missing tenant id: %v", details)
	}
}

func TestBootstrap_RejectsSecondTenant_NoStateMutated(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	first := core.BootstrapClaim{OID: "u-a", UPN: "a@a.onmicrosoft.com", TID: "tenant-a"}
	if _, err := core.Bootstrap(ctx, db, first); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}

	second := core.BootstrapClaim{OID: "u-b", UPN: "b@b.onmicrosoft.com", TID: "tenant-b"}
	if _, err := core.Bootstrap(ctx, db, second); !errors.Is(err, core.ErrAlreadyBound) {
		t.Fatalf("second bootstrap: got %v want ErrAlreadyBound", err)
	}

	var tid string
	if err := db.QueryRowContext(ctx, `SELECT tenant_id FROM config WHERE id=1`).Scan(&tid); err != nil {
		t.Fatal(err)
	}
	if tid != "tenant-a" {
		t.Errorf("tenant_id mutated by failed bootstrap: got %q", tid)
	}

	var stewards int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stewards`).Scan(&stewards); err != nil {
		t.Fatal(err)
	}
	if stewards != 1 {
		t.Errorf("steward count: got %d want 1", stewards)
	}

	var auditRows int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE action='tenant.bootstrap'`,
	).Scan(&auditRows); err != nil {
		t.Fatal(err)
	}
	if auditRows != 1 {
		t.Errorf("bootstrap audit rows: got %d want 1", auditRows)
	}
}

func TestBootstrap_RequiresNonEmptyFields(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	cases := []core.BootstrapClaim{
		{OID: "", UPN: "u@x", TID: "t"},
		{OID: "u", UPN: "", TID: "t"},
		{OID: "u", UPN: "u@x", TID: ""},
	}
	for _, c := range cases {
		if _, err := core.Bootstrap(ctx, db, c); err == nil {
			t.Errorf("expected error for claim %+v, got nil", c)
		}
	}
}
