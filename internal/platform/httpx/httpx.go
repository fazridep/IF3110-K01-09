// Package httpx berisi potongan HTTP yang dipakai bareng semua fitur:
// helper API JSON dan parsing id dari path.
//
// Package ini sengaja nggak import package internal lain, supaya bisa dipakai
// siapa saja (termasuk auth) tanpa bikin import cycle (ADR-012).
package httpx

import (
	"net/http"

	"github.com/google/uuid"
)

func PathUUID(r *http.Request, name string) (id uuid.UUID, ok bool) {
	id, err := uuid.Parse(r.PathValue(name))
	return id, err == nil
}
