package api

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/excelano/xensus/auth"
	"github.com/excelano/xensus/core"
	"github.com/excelano/xensus/id"
	"github.com/excelano/xensus/store"
)

// maxBodyBytes caps request bodies on the person write endpoints. Person
// payloads are tiny; this just stops a client from streaming megabytes
// into a name field.
const maxBodyBytes = 1 << 20 // 1 MiB

// personDTO is the JSON shape for a person. The ID is always the canonical
// "X-000123" string; timestamps and actor UPNs pass through as stored.
type personDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

func toPersonDTO(p store.Person) personDTO {
	return personDTO{
		ID:        id.Format(p.ID),
		Name:      p.Name,
		CreatedAt: p.CreatedAt,
		CreatedBy: p.CreatedBy,
		UpdatedAt: p.UpdatedAt,
		UpdatedBy: p.UpdatedBy,
	}
}

// personInput is the accepted body for create and rename.
type personInput struct {
	Name string `json:"name"`
}

// ListPersons handles GET /api/v1/persons[?q=…]. Open to any signed-in
// user. The optional q does a case-insensitive substring match on name.
func (h *Handlers) ListPersons(w http.ResponseWriter, r *http.Request) {
	persons, err := store.ListPersons(r.Context(), h.DB, r.URL.Query().Get("q"))
	if err != nil {
		httpError(r.Context(), w, err)
		return
	}
	out := make([]personDTO, 0, len(persons))
	for _, p := range persons {
		out = append(out, toPersonDTO(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"persons": out})
}

// GetPerson handles GET /api/v1/persons/{id}. Open to any signed-in user.
func (h *Handlers) GetPerson(w http.ResponseWriter, r *http.Request) {
	pid, err := id.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := store.GetPerson(r.Context(), h.DB, pid)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		httpError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPersonDTO(p))
}

// CreatePerson handles POST /api/v1/persons. Steward-gated.
func (h *Handlers) CreatePerson(w http.ResponseWriter, r *http.Request) {
	in, ok := decodePersonInput(w, r)
	if !ok {
		return
	}
	actor := actorFrom(r)
	p, err := core.CreatePerson(r.Context(), h.DB, actor, in.Name)
	if err != nil {
		httpError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toPersonDTO(p))
}

// RenamePerson handles PATCH /api/v1/persons/{id}. Steward-gated.
func (h *Handlers) RenamePerson(w http.ResponseWriter, r *http.Request) {
	pid, err := id.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	in, ok := decodePersonInput(w, r)
	if !ok {
		return
	}
	p, err := core.RenamePerson(r.Context(), h.DB, actorFrom(r), pid, in.Name)
	if err != nil {
		httpError(r.Context(), w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPersonDTO(p))
}

// ExportCSV handles GET /api/v1/persons.csv. Open to any signed-in user.
// Streams the full person list (respecting ?q=) as a CSV download.
func (h *Handlers) ExportCSV(w http.ResponseWriter, r *http.Request) {
	persons, err := store.ListPersons(r.Context(), h.DB, r.URL.Query().Get("q"))
	if err != nil {
		httpError(r.Context(), w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="persons.csv"`)

	cw := csv.NewWriter(w)
	writePersonsCSV(cw, persons)
	cw.Flush()
}

// decodePersonInput reads and validates the JSON body for create/rename.
// On failure it has already written the error response and returns false.
func decodePersonInput(w http.ResponseWriter, r *http.Request) (personInput, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in personInput
	if err := dec.Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return personInput{}, false
	}
	return in, true
}

// actorFrom builds a core.Actor from the authenticated user. The auth
// middleware guarantees a user is present on these routes.
func actorFrom(r *http.Request) core.Actor {
	u, _ := auth.UserFrom(r.Context())
	return core.Actor{OID: u.OID, UPN: u.UPN}
}
