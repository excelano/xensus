package api

import (
	"database/sql"
	"encoding/csv"
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

// systemDTO is the JSON shape for a system. Systems have no portable
// handle, so the ID is the bare numeric key; the disabled_* fields are
// omitted for an active system.
type systemDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at"`
	CreatedBy  string `json:"created_by"`
	UpdatedAt  string `json:"updated_at"`
	UpdatedBy  string `json:"updated_by"`
	DisabledAt string `json:"disabled_at,omitempty"`
	DisabledBy string `json:"disabled_by,omitempty"`
}

func toSystemDTO(s store.System) systemDTO {
	return systemDTO{
		ID:         s.ID,
		Name:       s.Name,
		CreatedAt:  s.CreatedAt,
		CreatedBy:  s.CreatedBy,
		UpdatedAt:  s.UpdatedAt,
		UpdatedBy:  s.UpdatedBy,
		DisabledAt: s.DisabledAt,
		DisabledBy: s.DisabledBy,
	}
}

// systemInput is the accepted body for create and rename.
type systemInput struct {
	Name string `json:"name"`
}

// ListSystems handles GET /api/v1/systems[?q=…]. Open to any signed-in
// user. Returns active (not disabled) systems; the optional q does a
// case-insensitive substring match on name.
func (h *Handlers) ListSystems(w http.ResponseWriter, r *http.Request) {
	systems, err := store.ListSystems(r.Context(), h.DB, r.URL.Query().Get("q"))
	if err != nil {
		httpError(w, err)
		return
	}
	out := make([]systemDTO, 0, len(systems))
	for _, s := range systems {
		out = append(out, toSystemDTO(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"systems": out})
}

// ListDisabledSystems handles GET /api/v1/systems/disabled. Open to any
// signed-in user. Returns disabled systems, most recently disabled first.
func (h *Handlers) ListDisabledSystems(w http.ResponseWriter, r *http.Request) {
	systems, err := store.ListDisabledSystems(r.Context(), h.DB)
	if err != nil {
		httpError(w, err)
		return
	}
	out := make([]systemDTO, 0, len(systems))
	for _, s := range systems {
		out = append(out, toSystemDTO(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"systems": out})
}

// GetSystem handles GET /api/v1/systems/{id}. Open to any signed-in user.
// A disabled system is still returned (with its disabled_* fields set).
func (h *Handlers) GetSystem(w http.ResponseWriter, r *http.Request) {
	sid, err := parseSystemID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s, err := store.GetSystem(r.Context(), h.DB, sid)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSystemDTO(s))
}

// CreateSystem handles POST /api/v1/systems. Steward-gated.
func (h *Handlers) CreateSystem(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeSystemInput(w, r)
	if !ok {
		return
	}
	s, err := core.CreateSystem(r.Context(), h.DB, actorFrom(r), in.Name)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toSystemDTO(s))
}

// RenameSystem handles PATCH /api/v1/systems/{id}. Steward-gated.
func (h *Handlers) RenameSystem(w http.ResponseWriter, r *http.Request) {
	sid, err := parseSystemID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in, ok := decodeSystemInput(w, r)
	if !ok {
		return
	}
	s, err := core.RenameSystem(r.Context(), h.DB, actorFrom(r), sid, in.Name)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSystemDTO(s))
}

// DisableSystem handles POST /api/v1/systems/{id}/disable. Steward-gated.
// The system is dropped from the active set but kept on record; the
// operation is idempotent. The response carries the system with its
// disabled_* fields populated.
func (h *Handlers) DisableSystem(w http.ResponseWriter, r *http.Request) {
	sid, err := parseSystemID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s, err := core.DisableSystem(r.Context(), h.DB, actorFrom(r), sid)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSystemDTO(s))
}

// EnableSystem handles POST /api/v1/systems/{id}/enable. Steward-gated.
// Returns a disabled system to the active set; idempotent.
func (h *Handlers) EnableSystem(w http.ResponseWriter, r *http.Request) {
	sid, err := parseSystemID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s, err := core.EnableSystem(r.Context(), h.DB, actorFrom(r), sid)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSystemDTO(s))
}

// ExportSystemsCSV handles GET /api/v1/systems.csv. Open to any signed-in
// user. Streams the active system list (respecting ?q=) as a CSV download.
func (h *Handlers) ExportSystemsCSV(w http.ResponseWriter, r *http.Request) {
	systems, err := store.ListSystems(r.Context(), h.DB, r.URL.Query().Get("q"))
	if err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="systems.csv"`)

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "name", "created_at", "created_by", "updated_at", "updated_by"})
	for _, s := range systems {
		_ = cw.Write([]string{
			strconv.FormatInt(s.ID, 10), s.Name, s.CreatedAt, s.CreatedBy, s.UpdatedAt, s.UpdatedBy,
		})
	}
	cw.Flush()
}

// parseSystemID reads a bare positive integer system id from a path value.
// Systems have no canonical handle, so this is plain strconv rather than
// id.Parse.
func parseSystemID(s string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid system id %q", s)
	}
	return n, nil
}

// decodeSystemInput reads and validates the JSON body for create/rename.
// On failure it has already written the error response and returns false.
func decodeSystemInput(w http.ResponseWriter, r *http.Request) (systemInput, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in systemInput
	if err := dec.Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return systemInput{}, false
	}
	return in, true
}
