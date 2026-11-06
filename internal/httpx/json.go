package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
)

const (
	ContentTypeJSON    = "application/json"
	ContentTypeProblem = "application/problem+json"

	MaxBodyBytes = 1 << 20
)

// List amplop koleksi, pakai NewList supaya slice nil tetap jadi [].
type List[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}

func NewList[T any](items []T) List[T] {
	if items == nil {
		items = []T{}
	}
	return List[T]{Items: items}
}

type JobAccepted struct {
	JobID     string `json:"jobId"`
	Status    string `json:"status"`
	StatusURL string `json:"statusUrl"`
}

// Problem error API format RFC 9457 dengan Type "urn:problem:<kode>".
type Problem struct {
	Type      string       `json:"type"`
	Title     string       `json:"title"`
	Status    int          `json:"status"`
	Detail    string       `json:"detail,omitempty"`
	Instance  string       `json:"instance,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
	RequestID string       `json:"requestId,omitempty"`
}

type FieldError struct {
	Field  string `json:"field"`
	Detail string `json:"detail"`
}

const (
	CodeInvalidInput     = "invalid-input"
	CodeUnauthenticated  = "unauthenticated"
	CodeForbidden        = "forbidden"
	CodeNotFound         = "not-found"
	CodeMethodNotAllowed = "method-not-allowed"
	CodeConflict         = "conflict"
	CodeUnsupportedMedia = "unsupported-media-type"
	CodeTooLarge         = "payload-too-large"
	CodeInternal         = "internal"
	CodeUnavailable      = "unavailable"
)

var defaultTitles = map[int]string{
	http.StatusBadRequest:            "Input tidak valid",
	http.StatusUnauthorized:          "Perlu login",
	http.StatusForbidden:             "Akses ditolak",
	http.StatusNotFound:              "Tidak ditemukan",
	http.StatusMethodNotAllowed:      "Method tidak didukung",
	http.StatusConflict:              "Konflik dengan keadaan saat ini",
	http.StatusRequestEntityTooLarge: "Body terlalu besar",
	http.StatusUnsupportedMediaType:  "Format body tidak didukung",
	http.StatusUnprocessableEntity:   "Tidak bisa diproses",
	http.StatusInternalServerError:   "Terjadi kesalahan di server",
	http.StatusBadGateway:            "Database tujuan bermasalah",
	http.StatusServiceUnavailable:    "Layanan belum siap",
}

func JSON(w http.ResponseWriter, status int, v any) {
	writeJSON(w, status, ContentTypeJSON, v)
}

func Created(w http.ResponseWriter, location string, v any) {
	if location != "" {
		w.Header().Set("Location", location)
	}
	JSON(w, http.StatusCreated, v)
}

// Accepted buat job yang baru masuk queue, belum tentu sukses.
func Accepted(w http.ResponseWriter, job JobAccepted) {
	if job.Status == "" {
		job.Status = "queued"
	}
	if job.StatusURL != "" {
		w.Header().Set("Location", job.StatusURL)
	}
	JSON(w, http.StatusAccepted, job)
}

func NoContent(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, contentType string, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		slog.Error("httpx: encode JSON gagal", "event", "http.encode_error", "error", err.Error())
		status, contentType = http.StatusInternalServerError, ContentTypeProblem
		body = []byte(`{"type":"urn:problem:internal","title":"Terjadi kesalahan di server","status":500}`)
	}
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

func WriteProblem(w http.ResponseWriter, r *http.Request, p Problem) {
	if p.Title == "" {
		p.Title = defaultTitles[p.Status]
		if p.Title == "" {
			p.Title = http.StatusText(p.Status)
		}
	}
	if p.Type == "" {
		p.Type = "about:blank"
	}
	if p.Instance == "" && r != nil {
		p.Instance = r.URL.Path
	}
	if p.RequestID == "" {
		p.RequestID = w.Header().Get("X-Request-ID")
	}
	writeJSON(w, p.Status, ContentTypeProblem, p)
}

func Fail(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	WriteProblem(w, r, Problem{Type: "urn:problem:" + code, Status: status, Detail: detail})
}

func Invalid(w http.ResponseWriter, r *http.Request, detail string, fields ...FieldError) {
	WriteProblem(w, r, Problem{Type: "urn:problem:" + CodeInvalidInput, Status: http.StatusBadRequest, Detail: detail, Errors: fields})
}

// NotFound juga dipakai buat resource yang ada tapi user nggak punya akses.
func NotFound(w http.ResponseWriter, r *http.Request, detail string) {
	Fail(w, r, http.StatusNotFound, CodeNotFound, detail)
}

// ServerError nyatat err di log, client cuma dapat 500 tanpa detail.
func ServerError(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "error di handler", "event", "http.server_error", "error", err.Error(), "path", r.URL.Path)
	Fail(w, r, http.StatusInternalServerError, CodeInternal, "")
}

// Decode baca body JSON ke dst, kalau gagal problem sudah ditulis dan hasilnya false.
func Decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil || mt != ContentTypeJSON {
			Fail(w, r, http.StatusUnsupportedMediaType, CodeUnsupportedMedia, "Body harus application/json.")
			return false
		}
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeDecodeError(w, r, err)
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		Invalid(w, r, "Body hanya boleh berisi satu objek JSON.")
		return false
	}
	return true
}

func writeDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	var (
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
		maxErr    *http.MaxBytesError
	)
	switch {
	case errors.As(err, &maxErr):
		Fail(w, r, http.StatusRequestEntityTooLarge, CodeTooLarge, fmt.Sprintf("Body maksimal %d byte.", maxErr.Limit))
	case errors.Is(err, io.EOF):
		Invalid(w, r, "Body kosong.")
	case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
		Invalid(w, r, "Body bukan JSON yang valid.")
	case errors.As(err, &typeErr):
		Invalid(w, r, "Tipe data salah.", FieldError{Field: typeErr.Field, Detail: "harus bertipe " + typeErr.Type.String()})
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		field := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		Invalid(w, r, "Ada field yang tidak dikenal.", FieldError{Field: field, Detail: "field tidak dikenal"})
	default:
		Invalid(w, r, "Body tidak bisa dibaca.")
	}
}