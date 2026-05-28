package web

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/excelano/xensus/auth"
	"github.com/excelano/xensus/core"
	"github.com/excelano/xensus/id"
	"github.com/excelano/xensus/store"
)

// listView is the template data for the persons list page.
type listView struct {
	Title   string
	User    *auth.User
	Query   string
	Persons []personRow
}

type personRow struct {
	ID   string // canonical "X-000123"
	Name string
}

// detailView is the template data for a single person page. Associations
// are the person's current links; Systems is the active-system list that
// fills the "associate with a system" dropdown (steward-only).
type detailView struct {
	Title        string
	User         *auth.User
	Person       personView
	Associations []associationRow
	Systems      []systemOption
	Audit        []auditView
}

type personView struct {
	ID        string
	Name      string
	CreatedAt string
	CreatedBy string
	UpdatedAt string
	UpdatedBy string
}

type auditView struct {
	OccurredAt string
	Action     string
	ActorUPN   string
	Details    string
}

// ListPersons renders GET /persons, honoring an optional ?q= search.
func (h *Handlers) ListPersons(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	persons, err := store.ListPersons(r.Context(), h.DB, q)
	if err != nil {
		slog.Error("web list persons", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rows := make([]personRow, 0, len(persons))
	for _, p := range persons {
		rows = append(rows, personRow{ID: id.Format(p.ID), Name: p.Name})
	}
	u, _ := auth.UserFrom(r.Context())
	h.rd.render(w, http.StatusOK, "persons_list", listView{
		Title:   "Persons",
		User:    u,
		Query:   q,
		Persons: rows,
	})
}

// PersonDetail renders GET /persons/{id} with the person and their audit
// history. A bad or unknown id is a 404 — the page either exists or it
// doesn't, from the browser's point of view.
func (h *Handlers) PersonDetail(w http.ResponseWriter, r *http.Request) {
	pid, err := id.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := store.GetPerson(r.Context(), h.DB, pid)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("web get person", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	links, err := store.ListAssociationsForPerson(r.Context(), h.DB, pid)
	if err != nil {
		slog.Error("web list associations", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	systems, err := store.ListSystems(r.Context(), h.DB, "")
	if err != nil {
		slog.Error("web list systems for associate", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rows, err := store.ListAuditForEntity(r.Context(), h.DB, "person", pid)
	if err != nil {
		slog.Error("web person audit", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	u, _ := auth.UserFrom(r.Context())
	h.rd.render(w, http.StatusOK, "person_detail", detailView{
		Title:        personTitle(p),
		User:         u,
		Person:       toPersonView(p),
		Associations: toAssociationRows(links),
		Systems:      toSystemOptions(systems),
		Audit:        toAuditViews(rows),
	})
}

// CreatePerson handles POST /persons (steward-gated at the router). It
// creates the person and 303-redirects to the new detail page, so a
// refresh re-GETs rather than re-POSTs.
func (h *Handlers) CreatePerson(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	p, err := core.CreatePerson(r.Context(), h.DB, actorFrom(r), r.PostFormValue("name"))
	if err != nil {
		slog.Error("web create person", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/persons/"+id.Format(p.ID), http.StatusSeeOther)
}

// RenamePerson handles POST /persons/{id} (steward-gated at the router).
// A no-op rename is harmless — core treats it as such — and we redirect
// back to the detail page either way.
func (h *Handlers) RenamePerson(w http.ResponseWriter, r *http.Request) {
	pid, err := id.Parse(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	_, err = core.RenamePerson(r.Context(), h.DB, actorFrom(r), pid, r.PostFormValue("name"))
	if errors.Is(err, core.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("web rename person", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/persons/"+id.Format(pid), http.StatusSeeOther)
}

func toPersonView(p store.Person) personView {
	return personView{
		ID:        id.Format(p.ID),
		Name:      p.Name,
		CreatedAt: formatTS(p.CreatedAt),
		CreatedBy: p.CreatedBy,
		UpdatedAt: formatTS(p.UpdatedAt),
		UpdatedBy: p.UpdatedBy,
	}
}

func toAuditViews(rows []store.AuditRow) []auditView {
	out := make([]auditView, 0, len(rows))
	for _, a := range rows {
		out = append(out, auditView{
			OccurredAt: formatTS(a.OccurredAt),
			Action:     a.Action,
			ActorUPN:   a.ActorUPN,
			Details:    a.Details,
		})
	}
	return out
}

// personTitle is the <title> for a detail page: the name if set, else the
// canonical ID so the tab is never blank.
func personTitle(p store.Person) string {
	if p.Name != "" {
		return p.Name
	}
	return id.Format(p.ID)
}

// formatTS renders a stored RFC3339 timestamp as a compact UTC string for
// display. Anything that doesn't parse passes through unchanged rather
// than being dropped.
func formatTS(s string) string {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC().Format("2006-01-02 15:04 UTC")
	}
	return s
}

// actorFrom builds a core.Actor from the authenticated user. The steward
// middleware guarantees a user is present on the write routes.
func actorFrom(r *http.Request) core.Actor {
	u, _ := auth.UserFrom(r.Context())
	return core.Actor{OID: u.OID, UPN: u.UPN}
}
