package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/excelano/xensus/store"
)

// CreateSystem registers a new system of record and writes a system.create
// audit row in the same transaction. Unlike a person, a system must have a
// name: the name is the system's identity (there is no portable handle), so
// a blank name is rejected with ErrNameRequired.
func CreateSystem(ctx context.Context, db *sql.DB, actor Actor, name string) (s store.System, err error) {
	if actor.OID == "" {
		return store.System{}, fmt.Errorf("create system requires an actor OID")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return store.System{}, ErrNameRequired
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return store.System{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	id, err := store.InsertSystem(ctx, tx, name, actor.UPN)
	if err != nil {
		return store.System{}, err
	}
	if err = WriteAudit(ctx, tx, AuditEntry{
		Actor:      actor,
		Action:     "system.create",
		EntityType: "system",
		EntityID:   id,
		Details:    map[string]any{"name": name},
	}); err != nil {
		return store.System{}, err
	}

	s, err = store.GetSystem(ctx, tx, id)
	if err != nil {
		return store.System{}, fmt.Errorf("read back system: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return store.System{}, fmt.Errorf("commit create system: %w", err)
	}
	return s, nil
}

// RenameSystem changes an active system's name and records the before/after
// in a system.rename audit row. A disabled system's name is frozen until it
// is enabled again, so a rename targeting one returns ErrNotFound. Renaming
// to the current name is a no-op: no audit row is written. A blank name is
// rejected with ErrNameRequired.
func RenameSystem(ctx context.Context, db *sql.DB, actor Actor, id int64, name string) (s store.System, err error) {
	if actor.OID == "" {
		return store.System{}, fmt.Errorf("rename system requires an actor OID")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return store.System{}, ErrNameRequired
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return store.System{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	current, err := store.GetSystem(ctx, tx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.System{}, ErrNotFound
		}
		return store.System{}, fmt.Errorf("read system: %w", err)
	}
	if current.Disabled() {
		return store.System{}, ErrNotFound
	}
	if current.Name == name {
		// Unchanged — commit nothing, write no audit row.
		_ = tx.Rollback()
		err = nil
		return current, nil
	}

	if _, err = store.UpdateSystemName(ctx, tx, id, name, actor.UPN); err != nil {
		return store.System{}, err
	}
	if err = WriteAudit(ctx, tx, AuditEntry{
		Actor:      actor,
		Action:     "system.rename",
		EntityType: "system",
		EntityID:   id,
		Details:    map[string]any{"from": current.Name, "to": name},
	}); err != nil {
		return store.System{}, err
	}

	s, err = store.GetSystem(ctx, tx, id)
	if err != nil {
		return store.System{}, fmt.Errorf("read back system: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return store.System{}, fmt.Errorf("commit rename system: %w", err)
	}
	return s, nil
}

// DisableSystem disables a system and records a system.disable audit row.
// Disabling drops the system from the active set but keeps its row and
// history — it is not a delete. The operation is idempotent: an
// already-disabled system is returned unchanged with no second audit row,
// mirroring the no-op rename. Returns ErrNotFound if the system never
// existed.
func DisableSystem(ctx context.Context, db *sql.DB, actor Actor, id int64) (s store.System, err error) {
	if actor.OID == "" {
		return store.System{}, fmt.Errorf("disable system requires an actor OID")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return store.System{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	current, err := store.GetSystem(ctx, tx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.System{}, ErrNotFound
		}
		return store.System{}, fmt.Errorf("read system: %w", err)
	}
	if current.Disabled() {
		// Already disabled — idempotent, write no second audit row.
		_ = tx.Rollback()
		err = nil
		return current, nil
	}

	if _, err = store.DisableSystem(ctx, tx, id, actor.UPN); err != nil {
		return store.System{}, err
	}
	if err = WriteAudit(ctx, tx, AuditEntry{
		Actor:      actor,
		Action:     "system.disable",
		EntityType: "system",
		EntityID:   id,
		Details:    map[string]any{"name": current.Name},
	}); err != nil {
		return store.System{}, err
	}

	s, err = store.GetSystem(ctx, tx, id)
	if err != nil {
		return store.System{}, fmt.Errorf("read back system: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return store.System{}, fmt.Errorf("commit disable system: %w", err)
	}
	return s, nil
}

// EnableSystem re-enables a disabled system and records a system.enable
// audit row, returning it to the active set. Like disabling, it is
// idempotent: an already-active system is returned unchanged with no audit
// row. Returns ErrNotFound if the system never existed.
func EnableSystem(ctx context.Context, db *sql.DB, actor Actor, id int64) (s store.System, err error) {
	if actor.OID == "" {
		return store.System{}, fmt.Errorf("enable system requires an actor OID")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return store.System{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	current, err := store.GetSystem(ctx, tx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.System{}, ErrNotFound
		}
		return store.System{}, fmt.Errorf("read system: %w", err)
	}
	if !current.Disabled() {
		// Already active — idempotent, write no audit row.
		_ = tx.Rollback()
		err = nil
		return current, nil
	}

	if _, err = store.EnableSystem(ctx, tx, id); err != nil {
		return store.System{}, err
	}
	if err = WriteAudit(ctx, tx, AuditEntry{
		Actor:      actor,
		Action:     "system.enable",
		EntityType: "system",
		EntityID:   id,
		Details:    map[string]any{"name": current.Name},
	}); err != nil {
		return store.System{}, err
	}

	s, err = store.GetSystem(ctx, tx, id)
	if err != nil {
		return store.System{}, fmt.Errorf("read back system: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return store.System{}, fmt.Errorf("commit enable system: %w", err)
	}
	return s, nil
}
