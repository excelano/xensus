// Package static exposes the vendored CSS as an embedded filesystem so the
// single binary serves its own styling with no external dependency. axe.css
// and brand.css are vendored copies of /home/anderix/axe/axe.css and
// /home/anderix/excelano-brand/brand.css; re-copy them to update.
package static

import "embed"

//go:embed *.css
var FS embed.FS
