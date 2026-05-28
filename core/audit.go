// Package core holds Xensus's business rules. Every mutating function
// in this package opens its own *sql.Tx, calls into store/* helpers with
// that Tx, and writes a corresponding audit_log row in the same Tx via
// WriteAudit. Reads remain free to use *sql.DB.
package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Actor identifies the user responsible for a mutation. OID is the M365
// objectId GUID (stable across renames and tenant moves) and UPN is the
// human-readable principal name at the time of the action — both are
// captured so the audit log stays readable even if a UPN changes later.
type Actor struct {
	OID string
	UPN string
}

// AuditEntry is the payload for a single audit_log row. Every mutating
// core function takes one as a required parameter — the compiler is the
// invariant enforcer here: you can't write data without also producing
// an audit row.
type AuditEntry struct {
	Actor      Actor
	Action     string // dotted lowercase, e.g. "person.create", "system.rename"
	EntityType string // "person", "system", "association", "steward", "tenant"
	EntityID   int64  // 0 means "no specific entity ID" (e.g. tenant bootstrap)
	Details    any    // optional; marshaled to JSON if non-nil
}

// WriteAudit inserts an audit_log row inside the given transaction. The
// caller is expected to commit the Tx; if the Tx is rolled back the audit
// row goes with the data change, which is the point.
func WriteAudit(ctx context.Context, tx *sql.Tx, e AuditEntry) error {
	if e.Action == "" {
		return fmt.Errorf("audit entry missing Action")
	}
	if e.EntityType == "" {
		return fmt.Errorf("audit entry missing EntityType")
	}
	if e.Actor.OID == "" {
		return fmt.Errorf("audit entry missing Actor.OID")
	}

	var detailsJSON sql.NullString
	if e.Details != nil {
		b, err := json.Marshal(e.Details)
		if err != nil {
			return fmt.Errorf("marshal audit details: %w", err)
		}
		detailsJSON = sql.NullString{String: string(b), Valid: true}
	}
	var entityID sql.NullInt64
	if e.EntityID != 0 {
		entityID = sql.NullInt64{Int64: e.EntityID, Valid: true}
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO audit_log
			(occurred_at, actor_oid, actor_upn, action, entity_type, entity_id, details)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		time.Now().UTC().Format(time.RFC3339Nano),
		e.Actor.OID,
		e.Actor.UPN,
		e.Action,
		e.EntityType,
		entityID,
		detailsJSON,
	)
	if err != nil {
		return fmt.Errorf("insert audit_log: %w", err)
	}
	return nil
}
