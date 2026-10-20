package web

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/if3110/tubes-db-platform/internal/auth"
	assets "github.com/if3110/tubes-db-platform/web"
)

// Renderer menggabungkan tiap halaman di web/templates/<fitur>/*.html dengan
// layout. Nama halaman ditulis "<fitur>/<file>", misalnya "auth/login".
type Renderer struct {
	pages map[string]*template.Template
}

// view adalah data yang diterima template. Layout memakai User dan CSRFToken,
// halaman memakai Data.
type view struct {
	User      *auth.User
	CSRFToken string
	Data      any
}

func NewRenderer() (*Renderer, error) {
	layout, err := template.ParseFS(assets.Templates, "templates/layout/*.html")
	if err != nil {
		return nil, fmt.Errorf("web: parse layout: %w", err)
	}
	files, err := fs.Glob(assets.Templates, "templates/*/*.html")
	if err != nil {
		return nil, err
	}

	pages := make(map[string]*template.Template)
	for _, file := range files {
		if strings.HasPrefix(file, "templates/layout/") {
			continue
		}
		base, err := layout.Clone()
		if err != nil {
			return nil, err
		}
		page, err := base.ParseFS(assets.Templates, file)
		if err != nil {
			return nil, fmt.Errorf("web: parse %s: %w", file, err)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(file, "templates/"), ".html")
		pages[name] = page
	}
	return &Renderer{pages: pages}, nil
}

func (rn *Renderer) Render(w http.ResponseWriter, r *http.Request, name string, data any) {
	page, ok := rn.pages[name]
	if !ok {
		log.Printf("web: halaman %q tidak ada", name)
		http.Error(w, "terjadi kesalahan di server", http.StatusInternalServerError)
		return
	}

	// Render ke buffer dulu supaya kalau gagal di tengah, client nggak dapat
	// halaman setengah jadi.
	var buf bytes.Buffer
	err := page.ExecuteTemplate(&buf, "layout", view{
		User:      auth.CurrentUser(r.Context()),
		CSRFToken: auth.CSRFToken(r),
		Data:      data,
	})
	if err != nil {
		log.Printf("web: render %s: %v", name, err)
		http.Error(w, "terjadi kesalahan di server", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}
