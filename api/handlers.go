package api

import "database/sql"

// Handlers carries the dependencies shared by the db-backed JSON handlers
// (persons now; systems, associations, stewards, audit in later slices).
// Handlers that need nothing but request context — like Me — stay package
// functions.
type Handlers struct {
	DB *sql.DB
}

// New builds the api handler set around the given database.
func New(db *sql.DB) *Handlers {
	return &Handlers{DB: db}
}
