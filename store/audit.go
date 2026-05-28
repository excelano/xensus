package store

import (
	"context"
	"database/sql"
	"fmt"
)

// AuditRow mirrors a row of audit_log as needed for read-side display.
// Details holds the raw JSON text the writer stored (or empty when the
// entry carried none). The richer, filterable audit viewer arrives in a
// later slice; this is the entity-scoped history a detail page needs.
type AuditRow struct {
	ID         int64
	OccurredAt string
	ActorUPN   string
	Action     string
	Details    string
}

// ListAuditForEntity returns every audit_log row for one entity, newest
// first. It's a read, so it takes *sql.DB rather than a transaction.
func ListAuditForEntity(ctx context.Context, db *sql.DB, entityType string, entityID int64) ([]AuditRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, occurred_at, actor_upn, action, details
		FROM audit_log
		WHERE entity_type = ? AND entity_id = ?
		ORDER BY occurred_at DESC, id DESC
	`, entityType, entityID)
	if err != nil {
		return nil, fmt.Errorf("list audit for %s %d: %w", entityType, entityID, err)
	}
	defer rows.Close()

	var out []AuditRow
	for rows.Next() {
		var (
			a       AuditRow
			details sql.NullString
		)
		if err := rows.Scan(&a.ID, &a.OccurredAt, &a.ActorUPN, &a.Action, &details); err != nil {
			return nil, fmt.Errorf("scan audit row: %w", err)
		}
		a.Details = details.String
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit rows: %w", err)
	}
	return out, nil
}
