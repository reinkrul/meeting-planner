// Package web embeds and renders HTML templates.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
)

//go:embed templates/*.html
var tplFS embed.FS

// Renderer holds parsed templates. One template.Template per page, each
// composed with base.html so we can render either the full page or just
// the inner "content" block (for htmx fragments).
type Renderer struct {
	pages map[string]*template.Template
}

var pageNames = []string{
	"landing",
	"booking_form",
	"participant_row",
	"slots",
	"confirm",
	"admin_connect",
}

func NewRenderer() (*Renderer, error) {
	r := &Renderer{pages: map[string]*template.Template{}}
	for _, name := range pageNames {
		t, err := template.ParseFS(tplFS, "templates/base.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		r.pages[name] = t
	}
	return r, nil
}

// Render writes a full HTML page.
func (r *Renderer) Render(w io.Writer, name string, data any) error {
	t, ok := r.pages[name]
	if !ok {
		return fmt.Errorf("unknown template %q", name)
	}
	return t.ExecuteTemplate(w, "base", data)
}

// RenderFragment writes only the inner content block (for htmx swaps).
func (r *Renderer) RenderFragment(w io.Writer, name string, data any) error {
	t, ok := r.pages[name]
	if !ok {
		return fmt.Errorf("unknown template %q", name)
	}
	return t.ExecuteTemplate(w, "content", data)
}
