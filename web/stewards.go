package web

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/excelano/xensus/core"
	"github.com/excelano/xensus/store"
)

// stewardsView is the template data for the stewards page. It carries both
// the current active stewards and the outstanding invitations, plus the
// caller's User so the page can show the invite form and remove buttons only
// to stewards.
type stewardsView struct {
	BaseView
	Stewards []stewardWebRow
	Pending  []pendingWebRow
}

// stewardWebRow is one active steward. IsSelf marks the caller's own row so
// the template can suppress the Remove button there — a steward can't demote
// themselves.
type stewardWebRow struct {
	ID         int64
	UPN        string
	PromotedAt string
	PromotedBy string
	IsSelf     bool
}

// pendingWebRow is one outstanding invitation, shown with a "will become a
// steward on first sign-in" label and a Cancel action.
type pendingWebRow struct {
	ID        int64
	UPN       string
	InvitedAt string
	InvitedBy string
}

// Stewards renders GET /stewards: the current stewards and pending invites.
func (h *Handlers) Stewards(w http.ResponseWriter, r *http.Request) {
	stewards, err := store.ListActiveStewards(r.Context(), h.DB)
	if err != nil {
		slog.ErrorContext(r.Context(),"web list stewards", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	pending, err := store.ListPendingStewards(r.Context(), h.DB)
	if err != nil {
		slog.ErrorContext(r.Context(),"web list pending stewards", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	base := h.base(r, "Stewards")
	selfOID := ""
	if base.User != nil {
		selfOID = base.User.OID
	}
	h.rd.render(w, http.StatusOK, "stewards", stewardsView{
		BaseView: base,
		Stewards: toStewardWebRows(stewards, selfOID),
		Pending:  toPendingWebRows(pending),
	})
}

// AddSteward handles POST /stewards (steward-gated). It invites a UPN, which
// becomes an active steward when that user next signs in, and 303-redirects
// back to the stewards page.
func (h *Handlers) AddSteward(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	_, err := core.PromoteSteward(r.Context(), h.DB, actorFrom(r), r.PostFormValue("upn"))
	switch {
	case errors.Is(err, core.ErrUPNRequired):
		http.Error(w, "a UPN is required", http.StatusBadRequest)
		return
	case errors.Is(err, core.ErrAlreadySteward):
		http.Error(w, "that user is already a steward", http.StatusConflict)
		return
	case errors.Is(err, core.ErrAlreadyInvited):
		http.Error(w, "that user already has a pending invitation", http.StatusConflict)
		return
	case err != nil:
		slog.ErrorContext(r.Context(),"web promote steward", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/stewards", http.StatusSeeOther)
}

// RemoveSteward handles POST /stewards/{id}/remove (steward-gated). HTML forms
// can't issue DELETE, so the web uses POST with a /remove suffix; the API uses
// a real DELETE. Demotion is reversible (re-invite + re-claim), so unlike the
// association hard delete it carries no confirmation gate. A self-targeted
// remove is a 409.
func (h *Handlers) RemoveSteward(w http.ResponseWriter, r *http.Request) {
	sid, err := parseWebStewardID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, err = core.DemoteSteward(r.Context(), h.DB, actorFrom(r), sid)
	switch {
	case errors.Is(err, core.ErrNotFound):
		http.NotFound(w, r)
		return
	case errors.Is(err, core.ErrSelfRemoval):
		http.Error(w, "you cannot remove yourself — promote a successor first", http.StatusConflict)
		return
	case err != nil:
		slog.ErrorContext(r.Context(),"web demote steward", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/stewards", http.StatusSeeOther)
}

// CancelInvite handles POST /stewards/pending/{id}/cancel (steward-gated). It
// withdraws an outstanding invitation and 303-redirects back. Like demotion
// this is reversible (re-invite the UPN), so it carries no confirmation gate.
func (h *Handlers) CancelInvite(w http.ResponseWriter, r *http.Request) {
	pid, err := parseWebStewardID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = core.CancelStewardInvite(r.Context(), h.DB, actorFrom(r), pid)
	if errors.Is(err, core.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(),"web cancel invite", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/stewards", http.StatusSeeOther)
}

func toStewardWebRows(stewards []store.Steward, selfOID string) []stewardWebRow {
	rows := make([]stewardWebRow, 0, len(stewards))
	for _, s := range stewards {
		rows = append(rows, stewardWebRow{
			ID:         s.ID,
			UPN:        s.UserUPN,
			PromotedAt: formatTS(s.PromotedAt),
			PromotedBy: s.PromotedBy,
			IsSelf:     s.UserOID == selfOID,
		})
	}
	return rows
}

func toPendingWebRows(pending []store.PendingSteward) []pendingWebRow {
	rows := make([]pendingWebRow, 0, len(pending))
	for _, p := range pending {
		rows = append(rows, pendingWebRow{
			ID:        p.ID,
			UPN:       p.UserUPN,
			InvitedAt: formatTS(p.InvitedAt),
			InvitedBy: p.InvitedBy,
		})
	}
	return rows
}

// parseWebStewardID reads a bare positive integer steward id from a path
// value, mirroring the API's parser.
func parseWebStewardID(s string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid steward id %q", s)
	}
	return n, nil
}
