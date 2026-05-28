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

// secondActor is a distinct steward used where one steward acts on another
// (demotion, self-removal guard).
var secondActor = core.Actor{OID: "actor-oid-2", UPN: "second@x.onmicrosoft.com"}

// seedActiveSteward promotes a UPN and immediately claims it as the given
// actor, returning the new active steward's id. It models the full
// invite→sign-in path the production code takes.
func seedActiveSteward(t *testing.T, db *sql.DB, inviter core.Actor, claimant core.Actor) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := core.PromoteSteward(ctx, db, inviter, claimant.UPN); err != nil {
		t.Fatalf("seed promote: %v", err)
	}
	claimed, err := core.ClaimPendingSteward(ctx, db, claimant)
	if err != nil || !claimed {
		t.Fatalf("seed claim: claimed=%v err=%v", claimed, err)
	}
	stewards, err := store.ListActiveStewards(ctx, db)
	if err != nil {
		t.Fatalf("seed list: %v", err)
	}
	for _, st := range stewards {
		if st.UserOID == claimant.OID {
			return st.ID
		}
	}
	t.Fatalf("seeded steward %s not found", claimant.UPN)
	return 0
}

func TestPromoteSteward_WritesPendingAndAudit(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	p, err := core.PromoteSteward(ctx, db, testActor, "  newbie@x.onmicrosoft.com  ")
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if p.UserUPN != "newbie@x.onmicrosoft.com" {
		t.Errorf("upn not trimmed/stored: %q", p.UserUPN)
	}
	if p.InvitedBy != testActor.UPN {
		t.Errorf("invited_by: got %q want %q", p.InvitedBy, testActor.UPN)
	}

	var (
		entityType string
		actor      string
		details    sql.NullString
	)
	if err := db.QueryRowContext(ctx,
		`SELECT entity_type, actor_oid, details FROM audit_log WHERE action='steward.invite'`,
	).Scan(&entityType, &actor, &details); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if entityType != "steward" {
		t.Errorf("audit entity_type: got %q want steward", entityType)
	}
	if actor != testActor.OID {
		t.Errorf("audit actor: got %q want %q", actor, testActor.OID)
	}
	if !details.Valid || !strings.Contains(details.String, "newbie@x.onmicrosoft.com") {
		t.Errorf("audit details missing upn: %v", details)
	}
}

func TestPromoteSteward_BlankUPN(t *testing.T) {
	db := openTestDB(t)
	if _, err := core.PromoteSteward(context.Background(), db, testActor, "   "); !errors.Is(err, core.ErrUPNRequired) {
		t.Errorf("got %v, want ErrUPNRequired", err)
	}
}

func TestPromoteSteward_RequiresActor(t *testing.T) {
	db := openTestDB(t)
	if _, err := core.PromoteSteward(context.Background(), db, core.Actor{}, "x@y"); err == nil {
		t.Error("expected error for empty actor")
	}
}

func TestPromoteSteward_AlreadyInvited(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if _, err := core.PromoteSteward(ctx, db, testActor, "dupe@x"); err != nil {
		t.Fatalf("first promote: %v", err)
	}
	if _, err := core.PromoteSteward(ctx, db, testActor, "dupe@x"); !errors.Is(err, core.ErrAlreadyInvited) {
		t.Errorf("got %v, want ErrAlreadyInvited", err)
	}
	// And case-insensitively.
	if _, err := core.PromoteSteward(ctx, db, testActor, "DUPE@X"); !errors.Is(err, core.ErrAlreadyInvited) {
		t.Errorf("case-insensitive: got %v, want ErrAlreadyInvited", err)
	}
}

func TestPromoteSteward_AlreadyActiveSteward(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	seedActiveSteward(t, db, testActor, secondActor)

	if _, err := core.PromoteSteward(ctx, db, testActor, secondActor.UPN); !errors.Is(err, core.ErrAlreadySteward) {
		t.Errorf("got %v, want ErrAlreadySteward", err)
	}
}

func TestClaimPendingSteward_PromotesAndAudits(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if _, err := core.PromoteSteward(ctx, db, testActor, secondActor.UPN); err != nil {
		t.Fatalf("promote: %v", err)
	}
	claimed, err := core.ClaimPendingSteward(ctx, db, secondActor)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !claimed {
		t.Fatal("expected claimed=true")
	}

	active, err := store.IsActiveSteward(ctx, db, secondActor.OID)
	if err != nil {
		t.Fatalf("is active: %v", err)
	}
	if !active {
		t.Error("claimant is not an active steward")
	}

	// Pending invitation is consumed.
	if _, err := store.GetPendingStewardByUPN(ctx, db, secondActor.UPN); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("pending invite survived claim: %v", err)
	}

	// The promote audit row is the invitee acting, with the inviter recorded.
	var (
		actor   string
		details sql.NullString
	)
	if err := db.QueryRowContext(ctx,
		`SELECT actor_oid, details FROM audit_log WHERE action='steward.promote'`,
	).Scan(&actor, &details); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if actor != secondActor.OID {
		t.Errorf("promote audit actor: got %q want %q (invitee)", actor, secondActor.OID)
	}
	if !details.Valid || !strings.Contains(details.String, testActor.UPN) {
		t.Errorf("promote audit details missing inviter: %v", details)
	}

	// Both lifecycle events are on record.
	if n := auditCount(t, db, "steward.invite"); n != 1 {
		t.Errorf("invite audit rows: got %d want 1", n)
	}
	if n := auditCount(t, db, "steward.promote"); n != 1 {
		t.Errorf("promote audit rows: got %d want 1", n)
	}
}

func TestClaimPendingSteward_CaseInsensitiveMatch(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if _, err := core.PromoteSteward(ctx, db, testActor, "Mixed.Case@Contoso.com"); err != nil {
		t.Fatalf("promote: %v", err)
	}
	claimed, err := core.ClaimPendingSteward(ctx, db, core.Actor{OID: "oid-mixed", UPN: "mixed.case@contoso.com"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !claimed {
		t.Error("expected case-insensitive UPN to claim the invite")
	}
}

func TestClaimPendingSteward_NoInviteIsNoOp(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	claimed, err := core.ClaimPendingSteward(ctx, db, secondActor)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed {
		t.Error("expected claimed=false with no pending invite")
	}
}

func TestClaimPendingSteward_AlreadyActiveConsumesStaleInvite(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	seedActiveSteward(t, db, testActor, secondActor)

	// Force a pending row for an already-active steward via the store layer
	// (PromoteSteward would reject it), then claim.
	tx, _ := db.BeginTx(ctx, nil)
	if _, err := store.InsertPendingSteward(ctx, tx, secondActor.UPN, testActor.UPN); err != nil {
		t.Fatalf("seed stale pending: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	claimed, err := core.ClaimPendingSteward(ctx, db, secondActor)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed {
		t.Error("expected claimed=false for an already-active steward")
	}
	// Stale invite consumed; no duplicate active row.
	if _, err := store.GetPendingStewardByUPN(ctx, db, secondActor.UPN); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("stale invite survived: %v", err)
	}
	stewards, err := store.ListActiveStewards(ctx, db)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var count int
	for _, s := range stewards {
		if s.UserOID == secondActor.OID {
			count++
		}
	}
	if count != 1 {
		t.Errorf("active rows for OID: got %d want 1", count)
	}
}

func TestCancelStewardInvite_RemovesAndAudits(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	p, err := core.PromoteSteward(ctx, db, testActor, "invitee@x")
	if err != nil {
		t.Fatalf("promote: %v", err)
	}

	if err := core.CancelStewardInvite(ctx, db, testActor, p.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := store.GetPendingStewardByUPN(ctx, db, "invitee@x"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("invitation survived cancel: %v", err)
	}
	if n := auditCount(t, db, "steward.uninvite"); n != 1 {
		t.Errorf("uninvite audit rows: got %d want 1", n)
	}
	// The UPN can be invited again afterward.
	if _, err := core.PromoteSteward(ctx, db, testActor, "invitee@x"); err != nil {
		t.Errorf("re-invite after cancel failed: %v", err)
	}
}

func TestCancelStewardInvite_NotFound(t *testing.T) {
	db := openTestDB(t)
	if err := core.CancelStewardInvite(context.Background(), db, testActor, 9999); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestCancelStewardInvite_RequiresActor(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	p, err := core.PromoteSteward(ctx, db, testActor, "invitee@x")
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if err := core.CancelStewardInvite(ctx, db, core.Actor{}, p.ID); err == nil {
		t.Error("expected error for empty actor")
	}
}

func TestDemoteSteward_SoftRemovesAndAudits(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := seedActiveSteward(t, db, testActor, secondActor)

	s, err := core.DemoteSteward(ctx, db, testActor, id)
	if err != nil {
		t.Fatalf("demote: %v", err)
	}
	if !s.Removed() {
		t.Error("returned steward not marked removed")
	}
	active, err := store.IsActiveSteward(ctx, db, secondActor.OID)
	if err != nil {
		t.Fatalf("is active: %v", err)
	}
	if active {
		t.Error("demoted steward still active")
	}
	if n := auditCount(t, db, "steward.demote"); n != 1 {
		t.Errorf("demote audit rows: got %d want 1", n)
	}
}

func TestDemoteSteward_SelfRemovalRejected(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := seedActiveSteward(t, db, testActor, secondActor)

	// secondActor tries to demote their own row.
	if _, err := core.DemoteSteward(ctx, db, secondActor, id); !errors.Is(err, core.ErrSelfRemoval) {
		t.Errorf("got %v, want ErrSelfRemoval", err)
	}
	// Row untouched, no audit.
	active, _ := store.IsActiveSteward(ctx, db, secondActor.OID)
	if !active {
		t.Error("self-removal demoted the steward anyway")
	}
	if n := auditCount(t, db, "steward.demote"); n != 0 {
		t.Errorf("self-removal wrote %d demote audit rows, want 0", n)
	}
}

func TestDemoteSteward_NotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if _, err := core.DemoteSteward(ctx, db, testActor, 9999); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestDemoteSteward_AlreadyRemovedIsNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := seedActiveSteward(t, db, testActor, secondActor)
	if _, err := core.DemoteSteward(ctx, db, testActor, id); err != nil {
		t.Fatalf("first demote: %v", err)
	}
	if _, err := core.DemoteSteward(ctx, db, testActor, id); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound on second demote", err)
	}
}

func TestDemoteSteward_RequiresActor(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	id := seedActiveSteward(t, db, testActor, secondActor)
	if _, err := core.DemoteSteward(ctx, db, core.Actor{}, id); err == nil {
		t.Error("expected error for empty actor")
	}
}
