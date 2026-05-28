package web

import (
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/excelano/xensus/id"
	"github.com/excelano/xensus/store"
)

// auditEntityTypes are the entity kinds the filter dropdown offers, in the
// order they read most naturally. They mirror the entity_type values core
// writes (see core.AuditEntry).
var auditEntityTypes = []string{"person", "system", "association", "steward", "tenant"}

// auditPageView is the template data for the audit timeline page. Limited
// is true when the result hit the default row cap, so the page can hint
// that older entries exist beyond the window.
type auditPageView struct {
	BaseView
	Filter  auditFilterView
	Events  []auditEventRow
	CSVHref string
	Limited bool
}

// auditFilterView carries both the current filter selections (to re-fill
// the form) and the option sets that populate its dropdowns.
type auditFilterView struct {
	EntityType  string
	Actor       string
	From        string
	To          string
	EntityTypes []string
	Actors      []string
}

// auditEventRow is one rendered timeline row. EntityHref is empty for
// entity kinds that have no standalone page (association, steward, tenant),
// in which case the template shows EntityLabel as plain text.
type auditEventRow struct {
	OccurredAt  string
	ActorUPN    string
	Action      string
	EntityLabel string
	EntityHref  string
	Details     string
}

// Audit renders GET /audit: a filterable, newest-first view of audit_log.
// Open to any signed-in user. Filters (entity_type, actor, from, to) come
// from the query string so the page is shareable and bookmarkable.
func (h *Handlers) Audit(w http.ResponseWriter, r *http.Request) {
	f := auditFilterView{
		EntityType:  strings.TrimSpace(r.URL.Query().Get("entity_type")),
		Actor:       strings.TrimSpace(r.URL.Query().Get("actor")),
		From:        strings.TrimSpace(r.URL.Query().Get("from")),
		To:          strings.TrimSpace(r.URL.Query().Get("to")),
		EntityTypes: auditEntityTypes,
	}
	events, err := store.ListAudit(r.Context(), h.DB, store.AuditQuery{
		EntityType: f.EntityType,
		Actor:      f.Actor,
		From:       f.From,
		To:         f.To,
		Limit:      store.DefaultAuditLimit,
	})
	if err != nil {
		slog.ErrorContext(r.Context(),"web list audit", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	actors, err := store.ListAuditActors(r.Context(), h.DB)
	if err != nil {
		slog.ErrorContext(r.Context(),"web list audit actors", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	f.Actors = actors

	h.rd.render(w, http.StatusOK, "audit", auditPageView{
		BaseView: h.base(r, "Audit log"),
		Filter:   f,
		Events:   toAuditEventRows(events),
		CSVHref:  auditCSVHref(f),
		Limited:  len(events) >= store.DefaultAuditLimit,
	})
}

func toAuditEventRows(events []store.AuditEvent) []auditEventRow {
	out := make([]auditEventRow, 0, len(events))
	for _, e := range events {
		label, href := entityLabelHref(e.EntityType, e.EntityID)
		out = append(out, auditEventRow{
			OccurredAt:  formatTS(e.OccurredAt),
			ActorUPN:    e.ActorUPN,
			Action:      e.Action,
			EntityLabel: label,
			EntityHref:  href,
			Details:     e.Details,
		})
	}
	return out
}

// entityLabelHref renders the entity column for a timeline row, linking to
// the entity's detail page where one exists. Persons print their canonical
// X- handle; systems print their bare numeric id. Associations, stewards,
// and tenant rows have no standalone page, so they get a label but no link.
func entityLabelHref(entityType string, entityID int64) (label, href string) {
	if entityID <= 0 {
		return entityType, ""
	}
	switch entityType {
	case "person":
		h := id.Format(entityID)
		return "person " + h, "/persons/" + h
	case "system":
		n := strconv.FormatInt(entityID, 10)
		return "system " + n, "/systems/" + n
	default:
		return entityType + " " + strconv.FormatInt(entityID, 10), ""
	}
}

// auditCSVHref points the page's download link at the JSON API's CSV
// endpoint, carrying the active filters so the export matches what's shown.
func auditCSVHref(f auditFilterView) string {
	v := url.Values{}
	if f.EntityType != "" {
		v.Set("entity_type", f.EntityType)
	}
	if f.Actor != "" {
		v.Set("actor", f.Actor)
	}
	if f.From != "" {
		v.Set("from", f.From)
	}
	if f.To != "" {
		v.Set("to", f.To)
	}
	if len(v) == 0 {
		return "/api/v1/audit.csv"
	}
	return "/api/v1/audit.csv?" + v.Encode()
}
