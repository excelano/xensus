// Package web owns Xensus's server-rendered HTML pages. It's HTML-first:
// ordinary forms that POST and 303-redirect, <details> for show/hide, no
// client-side framework. Handlers read the authenticated *auth.User from
// request context (the auth middleware populates it) and reuse the same
// core/* functions the JSON API does, so web and API stay behaviorally
// identical — including the audit-on-write invariant.
package web

import (
	"database/sql"

	"github.com/excelano/xensus/config"
)

// Handlers carries the dependencies shared by the HTML page handlers: the
// database, the parsed templates, and the read-access policy (used to decide
// which nav links a non-steward should see). New parses templates up front so
// a malformed template fails startup rather than a live request; Register sets
// the access policy when it wires the routes.
type Handlers struct {
	DB     *sql.DB
	rd     *renderer
	access config.Access
}

// New builds the web handler set, parsing all page templates. It returns
// an error if any template fails to parse.
func New(db *sql.DB) (*Handlers, error) {
	rd, err := newRenderer()
	if err != nil {
		return nil, err
	}
	return &Handlers{DB: db, rd: rd}, nil
}
