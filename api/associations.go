package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/excelano/xensus/core"
	"github.com/excelano/xensus/id"
	"github.com/excelano/xensus/store"
)

// associationDTO is the JSON shape for a person↔system link. PersonID is the
// canonical "X-000123" string; the system is identified by its bare numeric
// ID plus its name for convenience.
type associationDTO struct {
	ID        int64  `json:"id"`
	PersonID  string `json:"person_id"`
	SystemID  int64  `json:"system_id"`
	System    string `json:"system"`
	ForeignID string `json:"foreign_id"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
}

func toAssociationDTO(a store.Association) associationDTO {
	return associationDTO{
		ID:        a.ID,
		PersonID:  id.Format(a.PersonID),
		SystemID:  a.SystemID,
		System:    a.SystemName,
		ForeignID: a.ForeignID,
		CreatedAt: a.CreatedAt,
		CreatedBy: a.CreatedBy,
	}
}

// associationInput is the accepted body for creating a link.
type associationInput struct {
	SystemID  int64  `json:"system_id"`
	ForeignID string `json:"foreign_id"`
}

// ListAssociations handles GET /api/v1/persons/{id}/associations. Open to
// any signed-in user. A missing person is a 404 so a typo'd ID doesn't look
// like "a person with no links."
func (h *Handlers) ListAssociations(w http.ResponseWriter, r *http.Request) {
	pid, err := id.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := store.GetPerson(r.Context(), h.DB, pid); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		httpError(w, err)
		return
	}
	links, err := store.ListAssociationsForPerson(r.Context(), h.DB, pid)
	if err != nil {
		httpError(w, err)
		return
	}
	out := make([]associationDTO, 0, len(links))
	for _, a := range links {
		out = append(out, toAssociationDTO(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"associations": out})
}

// CreateAssociation handles POST /api/v1/persons/{id}/associations.
// Steward-gated. A missing person or system is a 404.
func (h *Handlers) CreateAssociation(w http.ResponseWriter, r *http.Request) {
	pid, err := id.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in, ok := decodeAssociationInput(w, r)
	if !ok {
		return
	}
	if in.SystemID <= 0 {
		writeError(w, http.StatusBadRequest, "system_id is required")
		return
	}
	a, err := core.CreateAssociation(r.Context(), h.DB, actorFrom(r), pid, in.SystemID, in.ForeignID)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAssociationDTO(a))
}

// RemoveAssociation handles DELETE /api/v1/persons/{id}/associations/{aid}.
// Steward-gated. The link is hard-deleted; the audit trail is the permanent
// record. Returns 204 on success, 404 if the link isn't found under that
// person.
func (h *Handlers) RemoveAssociation(w http.ResponseWriter, r *http.Request) {
	pid, err := id.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	aid, err := parseAssociationID(r.PathValue("aid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := core.RemoveAssociation(r.Context(), h.DB, actorFrom(r), pid, aid); err != nil {
		httpError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseAssociationID reads a bare positive integer association id from a path
// value. Associations have no canonical handle, so this is plain strconv.
func parseAssociationID(s string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid association id %q", s)
	}
	return n, nil
}

// decodeAssociationInput reads and validates the JSON body for create.
// On failure it has already written the error response and returns false.
func decodeAssociationInput(w http.ResponseWriter, r *http.Request) (associationInput, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in associationInput
	if err := dec.Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return associationInput{}, false
	}
	return in, true
}
