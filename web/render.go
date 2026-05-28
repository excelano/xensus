package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
)

//go:embed templates/*.html
var templatesFS embed.FS

// pageFiles maps a logical page name to its template file. Each page is
// parsed together with layout.html into its own template set, so the
// {{define "content"}} block in one page never collides with another's.
var pageFiles = map[string]string{
	"persons_list":  "templates/persons_list.html",
	"person_detail": "templates/person_detail.html",
	"systems_list":     "templates/systems_list.html",
	"system_detail":    "templates/system_detail.html",
	"disabled_systems": "templates/disabled_systems.html",
	"stewards":         "templates/stewards.html",
}

// renderer holds one fully-parsed template per page. Parsing happens once
// at startup; a parse failure there is a build-time fault surfaced as a
// startup error rather than a per-request panic.
type renderer struct {
	pages map[string]*template.Template
}

func newRenderer() (*renderer, error) {
	pages := make(map[string]*template.Template, len(pageFiles))
	for name, file := range pageFiles {
		t, err := template.New("layout.html").ParseFS(templatesFS, "templates/layout.html", file)
		if err != nil {
			return nil, fmt.Errorf("parse %s template: %w", name, err)
		}
		pages[name] = t
	}
	return &renderer{pages: pages}, nil
}

// render executes a page through layout.html into a buffer first, so a
// template error becomes a clean 500 instead of a half-written body with
// a 200 status already on the wire.
func (rd *renderer) render(w http.ResponseWriter, status int, page string, data any) {
	t, ok := rd.pages[page]
	if !ok {
		slog.Error("render: unknown page", "page", page)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", data); err != nil {
		slog.Error("render: execute template", "page", page, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
