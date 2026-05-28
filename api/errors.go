package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/excelano/xensus/core"
)

// writeJSON encodes v as the response body with the given status. Encoding
// errors are logged rather than surfaced — the status line is already on
// the wire by the time encoding could fail.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode json response", "err", err)
	}
}

// writeError sends a JSON {"error": msg} body with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// httpError maps a core sentinel error to the right HTTP status. Unknown
// errors become 500 and are logged, so an unexpected failure never leaks
// internal detail to the client but is still diagnosable from the logs.
func httpError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, core.ErrNameRequired):
		writeError(w, http.StatusBadRequest, "name is required")
	case errors.Is(err, core.ErrUPNRequired):
		writeError(w, http.StatusBadRequest, "upn is required")
	case errors.Is(err, core.ErrAlreadySteward):
		writeError(w, http.StatusConflict, "already a steward")
	case errors.Is(err, core.ErrAlreadyInvited):
		writeError(w, http.StatusConflict, "already invited")
	case errors.Is(err, core.ErrSelfRemoval):
		writeError(w, http.StatusConflict, "a steward cannot remove themselves")
	case errors.Is(err, core.ErrAlreadyBound):
		writeError(w, http.StatusConflict, "tenant already bound")
	default:
		slog.Error("unhandled api error", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}
