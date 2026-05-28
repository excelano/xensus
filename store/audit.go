package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
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

// Row caps for the cross-entity audit viewer. The timeline page uses the
// smaller default; CSV export raises the ceiling so a filtered download is
// closer to complete. Both are bounded so a query never loads the whole
// log into memory at once — a self-hosted registry's audit_log only grows.
const (
	DefaultAuditLimit = 200
	MaxAuditLimit     = 5000
)

// AuditEvent is a full audit_log row for the cross-entity timeline viewer.
// Unlike AuditRow (the entity-scoped detail-page history, where the entity
// is implied by context) it carries entity_type/entity_id and the actor's
// stable OID. EntityID is 0 when the row's entity_id is NULL (e.g. tenant
// bootstrap); person/system IDs start at 1, so 0 unambiguously means none.
type AuditEvent struct {
	ID         int64
	OccurredAt string
	ActorOID   string
	ActorUPN   string
	Action     string
	EntityType string
	EntityID   int64
	Details    string
}

// AuditQuery filters the audit timeline. A zero value matches everything
// (up to the default limit). EntityID is only applied alongside EntityType
// (an ID is meaningless without knowing which table it indexes). From/To
// are inclusive "YYYY-MM-DD" calendar days in UTC; anything that doesn't
// parse as a date is ignored rather than rejected.
type AuditQuery struct {
	EntityType string
	EntityID   int64
	Actor      string // exact actor_upn match
	From       string // inclusive lower-bound day
	To         string // inclusive upper-bound day
	Limit      int    // <=0 uses DefaultAuditLimit; capped at MaxAuditLimit
}

// ListAudit returns audit_log rows matching the filter, newest first.
func ListAudit(ctx context.Context, db *sql.DB, q AuditQuery) ([]AuditEvent, error) {
	var (
		conds []string
		args  []any
	)
	if et := strings.TrimSpace(q.EntityType); et != "" {
		conds = append(conds, "entity_type = ?")
		args = append(args, et)
		if q.EntityID > 0 {
			conds = append(conds, "entity_id = ?")
			args = append(args, q.EntityID)
		}
	}
	if a := strings.TrimSpace(q.Actor); a != "" {
		conds = append(conds, "actor_upn = ?")
		args = append(args, a)
	}
	if lo := dayLowerBound(q.From); lo != "" {
		conds = append(conds, "occurred_at >= ?")
		args = append(args, lo)
	}
	if hi := dayUpperBound(q.To); hi != "" {
		conds = append(conds, "occurred_at < ?")
		args = append(args, hi)
	}

	query := `SELECT id, occurred_at, actor_oid, actor_upn, action, entity_type, entity_id, details FROM audit_log`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY occurred_at DESC, id DESC LIMIT ?"
	args = append(args, auditLimit(q.Limit))

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()

	var out []AuditEvent
	for rows.Next() {
		var (
			e        AuditEvent
			entityID sql.NullInt64
			details  sql.NullString
		)
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.ActorOID, &e.ActorUPN, &e.Action, &e.EntityType, &entityID, &details); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		e.EntityID = entityID.Int64
		e.Details = details.String
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit events: %w", err)
	}
	return out, nil
}

// ListAuditActors returns the distinct actor UPNs that appear in the log,
// for the viewer's actor filter dropdown. The set is the (small) roster of
// everyone who has ever made a change, including demoted stewards.
func ListAuditActors(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT actor_upn FROM audit_log ORDER BY actor_upn COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list audit actors: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var upn string
		if err := rows.Scan(&upn); err != nil {
			return nil, fmt.Errorf("scan audit actor: %w", err)
		}
		out = append(out, upn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit actors: %w", err)
	}
	return out, nil
}

func auditLimit(n int) int {
	if n <= 0 {
		return DefaultAuditLimit
	}
	if n > MaxAuditLimit {
		return MaxAuditLimit
	}
	return n
}

// dayLowerBound validates a "YYYY-MM-DD" day and returns it unchanged for
// an occurred_at >= comparison. Stored timestamps are RFC3339 UTC, which
// sort lexically, so the bare date is a correct inclusive lower bound.
func dayLowerBound(s string) string {
	s = strings.TrimSpace(s)
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return ""
	}
	return s
}

// dayUpperBound turns a "YYYY-MM-DD" day into the next day, for an
// occurred_at < comparison — that makes the To filter inclusive of every
// timestamp on the given day without needing the time portion.
func dayUpperBound(s string) string {
	s = strings.TrimSpace(s)
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}
