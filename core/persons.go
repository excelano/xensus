package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/excelano/xensus/store"
)

// CreatePerson registers a new person and writes a person.create audit
// row in the same transaction. A blank name is allowed by design — the
// registry's job is to hand out a canonical ID, and the name can be
// filled in later. The created person (with its assigned ID and
// timestamps) is returned.
func CreatePerson(ctx context.Context, db *sql.DB, actor Actor, name string) (p store.Person, err error) {
	if actor.OID == "" {
		return store.Person{}, fmt.Errorf("create person requires an actor OID")
	}
	name = strings.TrimSpace(name)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return store.Person{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	id, err := store.InsertPerson(ctx, tx, name, actor.UPN)
	if err != nil {
		return store.Person{}, err
	}
	if err = WriteAudit(ctx, tx, AuditEntry{
		Actor:      actor,
		Action:     "person.create",
		EntityType: "person",
		EntityID:   id,
		Details:    map[string]any{"name": name},
	}); err != nil {
		return store.Person{}, err
	}

	p, err = store.GetPerson(ctx, tx, id)
	if err != nil {
		return store.Person{}, fmt.Errorf("read back person: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return store.Person{}, fmt.Errorf("commit create person: %w", err)
	}
	return p, nil
}

// RenamePerson changes a person's name and records the before/after in a
// person.rename audit row. Renaming to the current name is a no-op: the
// existing person is returned and no audit row is written, so a UI that
// re-submits an unchanged field doesn't pollute the timeline. Returns
// ErrNotFound if the person does not exist.
func RenamePerson(ctx context.Context, db *sql.DB, actor Actor, id int64, name string) (p store.Person, err error) {
	if actor.OID == "" {
		return store.Person{}, fmt.Errorf("rename person requires an actor OID")
	}
	name = strings.TrimSpace(name)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return store.Person{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	current, err := store.GetPerson(ctx, tx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Person{}, ErrNotFound
		}
		return store.Person{}, fmt.Errorf("read person: %w", err)
	}
	if current.Name == name {
		// Unchanged — commit nothing, write no audit row.
		_ = tx.Rollback()
		err = nil
		return current, nil
	}

	if _, err = store.UpdatePersonName(ctx, tx, id, name, actor.UPN); err != nil {
		return store.Person{}, err
	}
	if err = WriteAudit(ctx, tx, AuditEntry{
		Actor:      actor,
		Action:     "person.rename",
		EntityType: "person",
		EntityID:   id,
		Details:    map[string]any{"from": current.Name, "to": name},
	}); err != nil {
		return store.Person{}, err
	}

	p, err = store.GetPerson(ctx, tx, id)
	if err != nil {
		return store.Person{}, fmt.Errorf("read back person: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return store.Person{}, fmt.Errorf("commit rename person: %w", err)
	}
	return p, nil
}
