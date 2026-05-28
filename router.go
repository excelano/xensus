package main

import (
	"database/sql"
	"net/http"
)

// registerRoutes wires the top-level HTTP routes. The auth, api, and web
// subpackages get their own router files in subsequent slices and are
// composed into this mux here.
func registerRoutes(mux *http.ServeMux, db *sql.DB) {
	mux.HandleFunc("GET /health", healthHandler(db))
}
