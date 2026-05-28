package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/excelano/xensus/core"
	"github.com/excelano/xensus/store"
)

// stewardDTO is the JSON shape for an active steward. The stable identity is
// the M365 object id; user_upn is the readable principal captured at
// promotion time.
type stewardDTO struct {
	ID         int64  `json:"id"`
	OID        string `json:"oid"`
	UPN        string `json:"upn"`
	PromotedAt string `json:"promoted_at"`
	PromotedBy string `json:"promoted_by"`
}

// pendingStewardDTO is the JSON shape for an outstanding invitation, keyed by
// UPN until claimed at the invitee's sign-in.
type pendingStewardDTO struct {
	ID        int64  `json:"id"`
	UPN       string `json:"upn"`
	InvitedAt string `json:"invited_at"`
	InvitedBy string `json:"invited_by"`
}

// stewardInput is the accepted body for a promotion.
type stewardInput struct {
	UPN string `json:"upn"`
}

func toStewardDTO(s store.Steward) stewardDTO {
	return stewardDTO{ID: s.ID, OID: s.UserOID, UPN: s.UserUPN, PromotedAt: s.PromotedAt, PromotedBy: s.PromotedBy}
}

func toPendingStewardDTO(p store.PendingSteward) pendingStewardDTO {
	return pendingStewardDTO{ID: p.ID, UPN: p.UserUPN, InvitedAt: p.InvitedAt, InvitedBy: p.InvitedBy}
}

// ListStewards handles GET /api/v1/stewards. Open to any signed-in user. It
// returns both the current active stewards and the outstanding invitations
// that will become stewards on first sign-in.
func (h *Handlers) ListStewards(w http.ResponseWriter, r *http.Request) {
	stewards, err := store.ListActiveStewards(r.Context(), h.DB)
	if err != nil {
		httpError(r.Context(), w, err)
		return
	}
	pending, err := store.ListPendingStewards(r.Context(), h.DB)
	if err != nil {
		httpError(r.Context(), w, err)
		return
	}
	outStewards := make([]stewardDTO, 0, len(stewards))
	for _, s := range stewards {
		outStewards = append(outStewards, toStewardDTO(s))
	}
	outPending := make([]pendingStewardDTO, 0, len(pending))
	for _, p := range pending {
		outPending = append(outPending, toPendingStewardDTO(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"stewards": outStewards, "pending": outPending})
}

// PromoteSteward handles POST /api/v1/stewards. Steward-gated. The body
// carries the invitee's UPN; the response is the created pending invitation.
// A UPN that's already a steward or already invited is a 409.
func (h *Handlers) PromoteSteward(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeStewardInput(w, r)
	if !ok {
		return
	}
	p, err := core.PromoteSteward(r.Context(), h.DB, actorFrom(r), in.UPN)
	if err != nil {
		httpError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toPendingStewardDTO(p))
}

// DemoteSteward handles DELETE /api/v1/stewards/{id}. Steward-gated. A
// successful demote returns 204 No Content. A self-targeted demote is a 409
// (ErrSelfRemoval); a missing or already-demoted steward is a 404.
func (h *Handlers) DemoteSteward(w http.ResponseWriter, r *http.Request) {
	sid, err := parseStewardID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := core.DemoteSteward(r.Context(), h.DB, actorFrom(r), sid); err != nil {
		httpError(r.Context(), w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CancelInvite handles DELETE /api/v1/stewards/pending/{id}. Steward-gated. It
// withdraws an outstanding invitation; a successful cancel returns 204 No
// Content, a missing (or already-claimed) invitation is a 404.
func (h *Handlers) CancelInvite(w http.ResponseWriter, r *http.Request) {
	pid, err := parseStewardID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := core.CancelStewardInvite(r.Context(), h.DB, actorFrom(r), pid); err != nil {
		httpError(r.Context(), w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseStewardID reads a bare positive integer steward id from a path value.
func parseStewardID(s string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid steward id %q", s)
	}
	return n, nil
}

// decodeStewardInput reads and validates the JSON body for a promotion. On
// failure it has already written the error response and returns false.
func decodeStewardInput(w http.ResponseWriter, r *http.Request) (stewardInput, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in stewardInput
	if err := dec.Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return stewardInput{}, false
	}
	return in, true
}
