package web

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/excelano/xensus/core"
	"github.com/excelano/xensus/id"
	"github.com/excelano/xensus/store"
)

// associationRow is the template data for one of a person's links, shown in
// the associations table on the person detail page.
type associationRow struct {
	ID         int64
	SystemID   int64
	SystemName string
	ForeignID  string
	CreatedAt  string
	CreatedBy  string
}

// systemOption is one entry in the "associate with a system" dropdown. Only
// active systems are offered; an already-linked disabled system still shows
// in the list above, it just can't be the target of a new link from the UI.
type systemOption struct {
	ID   int64
	Name string
}

// AddAssociation handles POST /persons/{id}/associations (steward-gated). It
// links the person to the chosen system and 303-redirects back to the detail
// page. A missing person or system is a 404.
func (h *Handlers) AddAssociation(w http.ResponseWriter, r *http.Request) {
	pid, err := id.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	sid, err := strconv.ParseInt(strings.TrimSpace(r.PostFormValue("system_id")), 10, 64)
	if err != nil || sid <= 0 {
		http.Error(w, "a system is required", http.StatusBadRequest)
		return
	}
	_, err = core.CreateAssociation(r.Context(), h.DB, actorFrom(r), pid, sid, r.PostFormValue("foreign_id"))
	if errors.Is(err, core.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("web add association", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/persons/"+id.Format(pid), http.StatusSeeOther)
}

// RemoveAssociation handles POST /persons/{id}/associations/{aid}/remove
// (steward-gated). HTML forms can't issue DELETE, so the web uses POST with
// a /remove suffix; the API uses a real DELETE. The link is hard-deleted and
// we 303 back to the detail page.
func (h *Handlers) RemoveAssociation(w http.ResponseWriter, r *http.Request) {
	pid, err := id.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	aid, err := parseWebAssociationID(r.PathValue("aid"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = core.RemoveAssociation(r.Context(), h.DB, actorFrom(r), pid, aid)
	if errors.Is(err, core.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("web remove association", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/persons/"+id.Format(pid), http.StatusSeeOther)
}

func toAssociationRows(links []store.Association) []associationRow {
	rows := make([]associationRow, 0, len(links))
	for _, a := range links {
		rows = append(rows, associationRow{
			ID:         a.ID,
			SystemID:   a.SystemID,
			SystemName: a.SystemName,
			ForeignID:  a.ForeignID,
			CreatedAt:  formatTS(a.CreatedAt),
			CreatedBy:  a.CreatedBy,
		})
	}
	return rows
}

func toSystemOptions(systems []store.System) []systemOption {
	opts := make([]systemOption, 0, len(systems))
	for _, s := range systems {
		opts = append(opts, systemOption{ID: s.ID, Name: s.Name})
	}
	return opts
}

// parseWebAssociationID reads a bare positive integer association id from a
// path value, mirroring the API's parser.
func parseWebAssociationID(s string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid association id %q", s)
	}
	return n, nil
}
