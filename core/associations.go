package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/excelano/xensus/store"
)

// CreateAssociation links a person to a system, recording an
// association.create audit row in the same transaction. Both the person and
// the system must already exist (a missing either returns ErrNotFound). The
// foreign_id — the person's identifier in that system — is optional: the
// registry can note that a person shows up in a system before their ID there
// is known. Duplicate links are allowed by design, so this never dedupes.
//
// The audit row is filed against the person, not the association: an
// association can be removed (hard-deleted), and the person's timeline is
// where a steward looks to see where that person has shown up over time.
func CreateAssociation(ctx context.Context, db *sql.DB, actor Actor, personID, systemID int64, foreignID string) (a store.Association, err error) {
	if actor.OID == "" {
		return store.Association{}, fmt.Errorf("create association requires an actor OID")
	}
	foreignID = strings.TrimSpace(foreignID)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return store.Association{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = store.GetPerson(ctx, tx, personID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Association{}, ErrNotFound
		}
		return store.Association{}, fmt.Errorf("read person: %w", err)
	}
	system, err := store.GetSystem(ctx, tx, systemID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Association{}, ErrNotFound
		}
		return store.Association{}, fmt.Errorf("read system: %w", err)
	}

	id, err := store.InsertAssociation(ctx, tx, personID, systemID, foreignID, actor.UPN)
	if err != nil {
		return store.Association{}, err
	}
	if err = WriteAudit(ctx, tx, AuditEntry{
		Actor:      actor,
		Action:     "association.create",
		EntityType: "person",
		EntityID:   personID,
		Details: map[string]any{
			"association_id": id,
			"system_id":      systemID,
			"system":         system.Name,
			"foreign_id":     foreignID,
		},
	}); err != nil {
		return store.Association{}, err
	}

	a, err = store.GetAssociation(ctx, tx, id)
	if err != nil {
		return store.Association{}, fmt.Errorf("read back association: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return store.Association{}, fmt.Errorf("commit create association: %w", err)
	}
	return a, nil
}

// RemoveAssociation hard-deletes a person's link to a system and records an
// association.remove audit row (carrying the system name and foreign_id) in
// the same transaction. The row is gone for good; the audit trail is the
// permanent record, and re-adding the same link later makes a fresh row.
//
// assocID must belong to personID — an association under a different person
// (or one that never existed) returns ErrNotFound, so a caller can't remove
// links off another person's record by guessing IDs.
func RemoveAssociation(ctx context.Context, db *sql.DB, actor Actor, personID, assocID int64) (err error) {
	if actor.OID == "" {
		return fmt.Errorf("remove association requires an actor OID")
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

	current, err := store.GetAssociation(ctx, tx, assocID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read association: %w", err)
	}
	if current.PersonID != personID {
		return ErrNotFound
	}

	if _, err = store.DeleteAssociation(ctx, tx, assocID); err != nil {
		return err
	}
	if err = WriteAudit(ctx, tx, AuditEntry{
		Actor:      actor,
		Action:     "association.remove",
		EntityType: "person",
		EntityID:   personID,
		Details: map[string]any{
			"association_id": assocID,
			"system_id":      current.SystemID,
			"system":         current.SystemName,
			"foreign_id":     current.ForeignID,
		},
	}); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit remove association: %w", err)
	}
	return nil
}
