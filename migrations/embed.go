// Package migrations exposes the on-disk SQL migrations as an embedded
// filesystem so the store package can apply them without depending on the
// installation layout. See README.md for the append-only discipline.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
