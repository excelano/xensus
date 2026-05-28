package api

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/excelano/xensus/store"
)

// auditEventDTO is the JSON shape for one audit_log row. entity_id is
// omitted when zero (no specific entity). details is emitted as embedded
// JSON, not a quoted string, since the writer stored it as JSON; it is
// omitted entirely when the row carried none.
type auditEventDTO struct {
	ID         int64           `json:"id"`
	OccurredAt string          `json:"occurred_at"`
	ActorOID   string          `json:"actor_oid"`
	ActorUPN   string          `json:"actor_upn"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   int64           `json:"entity_id,omitempty"`
	Details    json.RawMessage `json:"details,omitempty"`
}

func toAuditEventDTO(e store.AuditEvent) auditEventDTO {
	dto := auditEventDTO{
		ID:         e.ID,
		OccurredAt: e.OccurredAt,
		ActorOID:   e.ActorOID,
		ActorUPN:   e.ActorUPN,
		Action:     e.Action,
		EntityType: e.EntityType,
		EntityID:   e.EntityID,
	}
	if e.Details != "" {
		dto.Details = json.RawMessage(e.Details)
	}
	return dto
}

// ListAudit handles GET /api/v1/audit. Open to any signed-in user — the
// registry is meant to be widely consultable. Filters come from the query
// string: entity_type, entity_id, actor, from, to, limit.
func (h *Handlers) ListAudit(w http.ResponseWriter, r *http.Request) {
	events, err := store.ListAudit(r.Context(), h.DB, auditQueryFrom(r))
	if err != nil {
		httpError(w, err)
		return
	}
	out := make([]auditEventDTO, 0, len(events))
	for _, e := range events {
		out = append(out, toAuditEventDTO(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

// ExportAuditCSV handles GET /api/v1/audit.csv. Same filters as ListAudit,
// streamed as a CSV download. When no explicit limit is given it raises the
// ceiling to MaxAuditLimit so an export is closer to the full filtered set
// than the smaller timeline default.
func (h *Handlers) ExportAuditCSV(w http.ResponseWriter, r *http.Request) {
	q := auditQueryFrom(r)
	if q.Limit <= 0 {
		q.Limit = store.MaxAuditLimit
	}
	events, err := store.ListAudit(r.Context(), h.DB, q)
	if err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit.csv"`)

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "occurred_at", "actor_oid", "actor_upn", "action", "entity_type", "entity_id", "details"})
	for _, e := range events {
		entityID := ""
		if e.EntityID > 0 {
			entityID = strconv.FormatInt(e.EntityID, 10)
		}
		_ = cw.Write([]string{
			strconv.FormatInt(e.ID, 10), e.OccurredAt, e.ActorOID, e.ActorUPN,
			e.Action, e.EntityType, entityID, e.Details,
		})
	}
	cw.Flush()
}

// auditQueryFrom builds a store.AuditQuery from the request's query string.
// entity_id and limit are tolerant: anything non-numeric is simply ignored,
// leaving that filter unset rather than erroring the whole request.
func auditQueryFrom(r *http.Request) store.AuditQuery {
	q := r.URL.Query()
	out := store.AuditQuery{
		EntityType: strings.TrimSpace(q.Get("entity_type")),
		Actor:      strings.TrimSpace(q.Get("actor")),
		From:       strings.TrimSpace(q.Get("from")),
		To:         strings.TrimSpace(q.Get("to")),
	}
	if eid, err := strconv.ParseInt(strings.TrimSpace(q.Get("entity_id")), 10, 64); err == nil && eid > 0 {
		out.EntityID = eid
	}
	if lim, err := strconv.Atoi(strings.TrimSpace(q.Get("limit"))); err == nil {
		out.Limit = lim
	}
	return out
}
