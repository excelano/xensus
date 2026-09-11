// Package static exposes the vendored CSS and brand marks as an embedded
// filesystem so the single binary serves its own styling and logo with no
// external dependency. axe.css and brand.css are vendored copies from the
// axe and excelano-brand repositories; the excelano-mark*.svg files derive
// from excelano-brand's logo directory. Re-copy them to update.
package static

import "embed"

//go:embed *.css *.svg
var FS embed.FS
