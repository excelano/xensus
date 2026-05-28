package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/excelano/xensus/store"
)

// PromoteSteward invites a user to become a steward, keyed by their UPN. The
// invitee's object ID isn't known until they sign in, so promotion records a
// pending_stewards row (with a steward.invite audit row in the same Tx) that
// is claimed at their next sign-in by ClaimPendingSteward. A UPN that already
// holds an active steward row returns ErrAlreadySteward; one that already has
// an outstanding invitation returns ErrAlreadyInvited. A blank UPN returns
// ErrUPNRequired.
func PromoteSteward(ctx context.Context, db *sql.DB, actor Actor, upn string) (p store.PendingSteward, err error) {
	if actor.OID == "" {
		return store.PendingSteward{}, fmt.Errorf("promote steward requires an actor OID")
	}
	upn = strings.TrimSpace(upn)
	if upn == "" {
		return store.PendingSteward{}, ErrUPNRequired
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return store.PendingSteward{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	already, err := store.IsActiveStewardByUPN(ctx, tx, upn)
	if err != nil {
		return store.PendingSteward{}, err
	}
	if already {
		return store.PendingSteward{}, ErrAlreadySteward
	}
	if _, err = store.GetPendingStewardByUPN(ctx, tx, upn); err == nil {
		return store.PendingSteward{}, ErrAlreadyInvited
	} else if !errors.Is(err, sql.ErrNoRows) {
		return store.PendingSteward{}, fmt.Errorf("check pending steward: %w", err)
	}

	if _, err = store.InsertPendingSteward(ctx, tx, upn, actor.UPN); err != nil {
		return store.PendingSteward{}, err
	}
	if err = WriteAudit(ctx, tx, AuditEntry{
		Actor:      actor,
		Action:     "steward.invite",
		EntityType: "steward",
		Details:    map[string]any{"upn": upn},
	}); err != nil {
		return store.PendingSteward{}, err
	}

	p, err = store.GetPendingStewardByUPN(ctx, tx, upn)
	if err != nil {
		return store.PendingSteward{}, fmt.Errorf("read back pending steward: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return store.PendingSteward{}, fmt.Errorf("commit promote steward: %w", err)
	}
	return p, nil
}

// DemoteSteward soft-removes an active steward and writes a steward.demote
// audit row in the same Tx. A steward cannot demote themselves — they must
// find a successor first — so a self-targeted demote returns ErrSelfRemoval.
// A missing or already-demoted steward returns ErrNotFound. Demotion is
// reversible: the person can be invited again and re-claims on next sign-in.
func DemoteSteward(ctx context.Context, db *sql.DB, actor Actor, stewardID int64) (s store.Steward, err error) {
	if actor.OID == "" {
		return store.Steward{}, fmt.Errorf("demote steward requires an actor OID")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return store.Steward{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	current, err := store.GetSteward(ctx, tx, stewardID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Steward{}, ErrNotFound
		}
		return store.Steward{}, fmt.Errorf("read steward: %w", err)
	}
	if current.Removed() {
		return store.Steward{}, ErrNotFound
	}
	if current.UserOID == actor.OID {
		return store.Steward{}, ErrSelfRemoval
	}

	if _, err = store.DemoteSteward(ctx, tx, stewardID, actor.UPN); err != nil {
		return store.Steward{}, err
	}
	if err = WriteAudit(ctx, tx, AuditEntry{
		Actor:      actor,
		Action:     "steward.demote",
		EntityType: "steward",
		EntityID:   stewardID,
		Details:    map[string]any{"oid": current.UserOID, "upn": current.UserUPN},
	}); err != nil {
		return store.Steward{}, err
	}

	s, err = store.GetSteward(ctx, tx, stewardID)
	if err != nil {
		return store.Steward{}, fmt.Errorf("read back steward: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return store.Steward{}, fmt.Errorf("commit demote steward: %w", err)
	}
	return s, nil
}

// CancelStewardInvite withdraws an outstanding invitation before it's claimed
// and writes a steward.uninvite audit row in the same Tx. A missing invitation
// — including one that's already been claimed (its pending row is gone) —
// returns ErrNotFound; to undo a claimed invite, demote the now-active steward
// instead. Cancelling is reversible: the UPN can simply be invited again.
func CancelStewardInvite(ctx context.Context, db *sql.DB, actor Actor, pendingID int64) (err error) {
	if actor.OID == "" {
		return fmt.Errorf("cancel invite requires an actor OID")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	pending, err := store.GetPendingStewardByID(ctx, tx, pendingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read pending steward: %w", err)
	}

	if _, err = store.DeletePendingSteward(ctx, tx, pendingID); err != nil {
		return err
	}
	if err = WriteAudit(ctx, tx, AuditEntry{
		Actor:      actor,
		Action:     "steward.uninvite",
		EntityType: "steward",
		Details:    map[string]any{"upn": pending.UserUPN},
	}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit cancel invite: %w", err)
	}
	return nil
}

// ClaimPendingSteward is called on every sign-in. If the signing-in user's
// UPN matches an outstanding invitation, it promotes them to an active
// steward, consumes the invitation, and writes a steward.promote audit row —
// all in one Tx — and reports claimed=true. The audit actor is the invitee
// (the person completing the action by signing in); the inviter is preserved
// as promoted_by and in the audit details. A sign-in with no matching
// invitation is the common case and is a cheap no-op (claimed=false). If the
// user is somehow already an active steward, the stale invitation is consumed
// without creating a duplicate row.
func ClaimPendingSteward(ctx context.Context, db *sql.DB, actor Actor) (claimed bool, err error) {
	if actor.OID == "" || actor.UPN == "" {
		return false, fmt.Errorf("claim steward requires actor OID and UPN")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	pending, err := store.GetPendingStewardByUPN(ctx, tx, actor.UPN)
	if errors.Is(err, sql.ErrNoRows) {
		// No invitation for this UPN — the common path. Roll back the
		// read-only Tx explicitly: returning a nil error here would skip the
		// deferred rollback and leak the connection on every ordinary sign-in.
		_ = tx.Rollback()
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look up pending steward: %w", err)
	}

	active, err := store.IsActiveSteward(ctx, tx, actor.OID)
	if err != nil {
		return false, err
	}
	if active {
		// Already a steward (e.g. invited redundantly) — consume the stale
		// invitation without inserting a duplicate active row.
		if _, err = store.DeletePendingSteward(ctx, tx, pending.ID); err != nil {
			return false, err
		}
		if err = tx.Commit(); err != nil {
			return false, fmt.Errorf("commit consume stale invite: %w", err)
		}
		return false, nil
	}

	stewardID, err := store.InsertSteward(ctx, tx, actor.OID, actor.UPN, pending.InvitedBy)
	if err != nil {
		return false, err
	}
	if _, err = store.DeletePendingSteward(ctx, tx, pending.ID); err != nil {
		return false, err
	}
	if err = WriteAudit(ctx, tx, AuditEntry{
		Actor:      actor,
		Action:     "steward.promote",
		EntityType: "steward",
		EntityID:   stewardID,
		Details:    map[string]any{"oid": actor.OID, "upn": actor.UPN, "invited_by": pending.InvitedBy},
	}); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit claim steward: %w", err)
	}
	return true, nil
}
