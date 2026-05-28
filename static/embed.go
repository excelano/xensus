// Package static exposes the vendored CSS and brand marks as an embedded
// filesystem so the single binary serves its own styling and logo with no
// external dependency. axe.css and brand.css are vendored copies of
// /home/anderix/axe/axe.css and /home/anderix/excelano-brand/brand.css; the
// excelano-mark*.svg files derive from /home/anderix/excelano-brand/logo/.
// Re-copy them to update.
package static

import "embed"

//go:embed *.css *.svg
var FS embed.FS
