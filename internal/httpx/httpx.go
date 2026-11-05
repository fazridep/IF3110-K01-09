// Package httpx berisi potongan HTTP yang dipakai bareng semua fitur:
// kontrak Renderer, halaman error, dan parsing id dari path.
//
// Package ini sengaja nggak import package internal lain, supaya bisa dipakai
// siapa saja (termasuk auth) tanpa bikin import cycle (ADR-012).
package httpx

import (
	"net/http"

	"github.com/google/uuid"
)

type Renderer interface {
	Render(w http.ResponseWriter, r *http.Request, name string, data any)
	RenderStatus(w http.ResponseWriter, r *http.Request, status int, name string, data any)
}

const ErrorTemplate = "shared/error"

type ErrorPage struct {
	Message   string
	BackURL   string
	BackLabel string
}

func Error(w http.ResponseWriter, r *http.Request, view Renderer, status int, page ErrorPage) {
	view.RenderStatus(w, r, status, ErrorTemplate, page)
}

func PathUUID(r *http.Request, name string) (id uuid.UUID, ok bool) {
	id, err := uuid.Parse(r.PathValue(name))
	return id, err == nil
}
