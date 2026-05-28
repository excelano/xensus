package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/excelano/xensus/core"
	"github.com/excelano/xensus/store"
)

// systemsListView is the template data for the systems list page.
// DisabledCount drives the "View N disabled" link to the disabled list.
type systemsListView struct {
	BaseView
	Query         string
	Systems       []systemRow
	DisabledCount int
}

// disabledSystemsView is the template data for the disabled list page.
type disabledSystemsView struct {
	BaseView
	Systems []systemRow
}

type systemRow struct {
	ID         int64
	Name       string
	CreatedAt  string
	CreatedBy  string
	DisabledAt string
	DisabledBy string
}

// systemDetailView is the template data for a single system page.
type systemDetailView struct {
	BaseView
	System systemView
	Audit  []auditView
}

type systemView struct {
	ID         int64
	Name       string
	CreatedAt  string
	CreatedBy  string
	UpdatedAt  string
	UpdatedBy  string
	Disabled   bool
	DisabledAt string
	DisabledBy string
}

// ListSystems renders GET /systems, honoring an optional ?q= search. It
// also counts disabled systems so the page can link to them.
func (h *Handlers) ListSystems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	systems, err := store.ListSystems(r.Context(), h.DB, q)
	if err != nil {
		slog.ErrorContext(r.Context(),"web list systems", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	disabledCount, err := store.CountDisabledSystems(r.Context(), h.DB)
	if err != nil {
		slog.ErrorContext(r.Context(),"web count disabled systems", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.rd.render(w, http.StatusOK, "systems_list", systemsListView{
		BaseView:      h.base(r, "Systems"),
		Query:         q,
		Systems:       toSystemRows(systems),
		DisabledCount: disabledCount,
	})
}

// ListDisabledSystems renders GET /systems/disabled, the steward-visible
// list of systems that have been disabled.
func (h *Handlers) ListDisabledSystems(w http.ResponseWriter, r *http.Request) {
	systems, err := store.ListDisabledSystems(r.Context(), h.DB)
	if err != nil {
		slog.ErrorContext(r.Context(),"web list disabled systems", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.rd.render(w, http.StatusOK, "disabled_systems", disabledSystemsView{
		BaseView: h.base(r, "Disabled systems"),
		Systems:  toSystemRows(systems),
	})
}

// SystemDetail renders GET /systems/{id} with the system and its audit
// history. A bad or unknown id is a 404. A disabled system still renders,
// showing a disabled banner, an Enable action, and its frozen name.
func (h *Handlers) SystemDetail(w http.ResponseWriter, r *http.Request) {
	sid, err := parseWebSystemID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s, err := store.GetSystem(r.Context(), h.DB, sid)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(),"web get system", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rows, err := store.ListAuditForEntity(r.Context(), h.DB, "system", sid)
	if err != nil {
		slog.ErrorContext(r.Context(),"web system audit", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.rd.render(w, http.StatusOK, "system_detail", systemDetailView{
		BaseView: h.base(r, s.Name),
		System:   toSystemView(s),
		Audit:    toAuditViews(rows),
	})
}

// CreateSystem handles POST /systems (steward-gated). The form's name input
// is marked required, so an empty name is normally caught client-side; the
// ErrNameRequired branch is the server-side backstop.
func (h *Handlers) CreateSystem(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	s, err := core.CreateSystem(r.Context(), h.DB, actorFrom(r), r.PostFormValue("name"))
	if errors.Is(err, core.ErrNameRequired) {
		http.Error(w, "system name is required", http.StatusBadRequest)
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(),"web create system", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/systems/"+strconv.FormatInt(s.ID, 10), http.StatusSeeOther)
}

// RenameSystem handles POST /systems/{id} (steward-gated).
func (h *Handlers) RenameSystem(w http.ResponseWriter, r *http.Request) {
	sid, err := parseWebSystemID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	_, err = core.RenameSystem(r.Context(), h.DB, actorFrom(r), sid, r.PostFormValue("name"))
	if errors.Is(err, core.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, core.ErrNameRequired) {
		http.Error(w, "system name is required", http.StatusBadRequest)
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(),"web rename system", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/systems/"+strconv.FormatInt(sid, 10), http.StatusSeeOther)
}

// DisableSystem handles POST /systems/{id}/disable (steward-gated). The
// system is kept on record, so no JS confirmation gate stands in front of
// it — and it's reversible via Enable.
func (h *Handlers) DisableSystem(w http.ResponseWriter, r *http.Request) {
	h.toggleSystem(w, r, core.DisableSystem, "web disable system")
}

// EnableSystem handles POST /systems/{id}/enable (steward-gated), returning
// a disabled system to the active set.
func (h *Handlers) EnableSystem(w http.ResponseWriter, r *http.Request) {
	h.toggleSystem(w, r, core.EnableSystem, "web enable system")
}

// toggleSystem is the shared body of the disable/enable handlers: both parse
// the id, call the given core toggle, and redirect back to the detail page.
func (h *Handlers) toggleSystem(
	w http.ResponseWriter, r *http.Request,
	toggle func(context.Context, *sql.DB, core.Actor, int64) (store.System, error),
	logMsg string,
) {
	sid, err := parseWebSystemID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, err = toggle(r.Context(), h.DB, actorFrom(r), sid)
	if errors.Is(err, core.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(),logMsg, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/systems/"+strconv.FormatInt(sid, 10), http.StatusSeeOther)
}

func toSystemRows(systems []store.System) []systemRow {
	rows := make([]systemRow, 0, len(systems))
	for _, s := range systems {
		rows = append(rows, systemRow{
			ID:         s.ID,
			Name:       s.Name,
			CreatedAt:  formatTS(s.CreatedAt),
			CreatedBy:  s.CreatedBy,
			DisabledAt: formatTS(s.DisabledAt),
			DisabledBy: s.DisabledBy,
		})
	}
	return rows
}

func toSystemView(s store.System) systemView {
	return systemView{
		ID:         s.ID,
		Name:       s.Name,
		CreatedAt:  formatTS(s.CreatedAt),
		CreatedBy:  s.CreatedBy,
		UpdatedAt:  formatTS(s.UpdatedAt),
		UpdatedBy:  s.UpdatedBy,
		Disabled:   s.Disabled(),
		DisabledAt: formatTS(s.DisabledAt),
		DisabledBy: s.DisabledBy,
	}
}

// parseWebSystemID reads a bare positive integer system id from a path
// value, mirroring the API's parser.
func parseWebSystemID(s string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid system id %q", s)
	}
	return n, nil
}
