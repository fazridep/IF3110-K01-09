package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func decodeProblem(t *testing.T, rr *httptest.ResponseRecorder) Problem {
	t.Helper()
	if ct := rr.Header().Get("Content-Type"); ct != ContentTypeProblem {
		t.Fatalf("Content-Type mau %s, dapat %q", ContentTypeProblem, ct)
	}
	var p Problem
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatalf("body problem bukan JSON: %v", err)
	}
	return p
}

func TestJSONListEmptyIsArray(t *testing.T) {
	rr := httptest.NewRecorder()
	JSON(rr, http.StatusOK, NewList[string](nil))

	if got := strings.TrimSpace(rr.Body.String()); got != `{"items":[]}` {
		t.Fatalf("koleksi kosong harus {\"items\":[]}, dapat %s", got)
	}
	if rr.Header().Get("Cache-Control") != "no-store" || rr.Header().Get("Content-Type") != ContentTypeJSON {
		t.Errorf("header JSON salah: %v", rr.Header())
	}
}

func TestJSONTimeIsRFC3339(t *testing.T) {
	rr := httptest.NewRecorder()
	JSON(rr, http.StatusOK, struct {
		CreatedAt time.Time `json:"createdAt"`
	}{time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)})
	if !strings.Contains(rr.Body.String(), `"createdAt":"2026-11-06T09:00:00Z"`) {
		t.Fatalf("waktu harus RFC 3339: %s", rr.Body.String())
	}
}

func TestCreatedAcceptedNoContent(t *testing.T) {
	rr := httptest.NewRecorder()
	Created(rr, "/api/groups/1", map[string]string{"id": "1"})
	if rr.Code != http.StatusCreated || rr.Header().Get("Location") != "/api/groups/1" {
		t.Errorf("Created: %d %v", rr.Code, rr.Header())
	}

	rr = httptest.NewRecorder()
	Accepted(rr, JobAccepted{JobID: "j1", StatusURL: "/api/jobs/j1"})
	var job JobAccepted
	_ = json.Unmarshal(rr.Body.Bytes(), &job)
	if rr.Code != http.StatusAccepted || job.Status != "queued" || job.JobID != "j1" || rr.Header().Get("Location") != "/api/jobs/j1" {
		t.Errorf("Accepted: %d %+v", rr.Code, job)
	}

	rr = httptest.NewRecorder()
	NoContent(rr)
	if rr.Code != http.StatusNoContent || rr.Body.Len() != 0 {
		t.Errorf("NoContent: %d %q", rr.Code, rr.Body.String())
	}
}

func TestProblem(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/migrations/9", nil)
	rr := httptest.NewRecorder()
	rr.Header().Set("X-Request-ID", "req-1")
	Fail(rr, req, http.StatusConflict, "migration-running", "Masih ada migration yang berjalan.")

	p := decodeProblem(t, rr)
	want := Problem{
		Type: "urn:problem:migration-running", Title: "Konflik dengan keadaan saat ini", Status: 409,
		Detail: "Masih ada migration yang berjalan.", Instance: "/api/migrations/9", RequestID: "req-1",
	}
	if rr.Code != 409 || p.Type != want.Type || p.Title != want.Title || p.Status != want.Status ||
		p.Detail != want.Detail || p.Instance != want.Instance || p.RequestID != want.RequestID {
		t.Fatalf("problem salah:\n dapat %+v\n mau   %+v", p, want)
	}
}

func TestServerErrorHidesDetail(t *testing.T) {
	rr := httptest.NewRecorder()
	ServerError(rr, httptest.NewRequest(http.MethodGet, "/api/x", nil), errors.New("pq: password authentication failed for user target"))
	if rr.Code != 500 || strings.Contains(rr.Body.String(), "password") {
		t.Fatalf("detail error internal nggak boleh dikirim ke client: %s", rr.Body.String())
	}
}

type input struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestDecode(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
		wantOK      bool
		wantStatus  int
		wantField   string
	}{
		{"valid", "application/json", `{"name":"a","count":2}`, true, 0, ""},
		{"charset boleh", "application/json; charset=utf-8", `{"name":"a"}`, true, 0, ""},
		{"tanpa content-type", "", `{"name":"a"}`, true, 0, ""},
		{"bukan json", "text/plain", `{"name":"a"}`, false, 415, ""},
		{"kosong", "application/json", ``, false, 400, ""},
		{"rusak", "application/json", `{"name":`, false, 400, ""},
		{"tipe salah", "application/json", `{"count":"dua"}`, false, 400, "count"},
		{"field asing", "application/json", `{"nama":"a"}`, false, 400, "nama"},
		{"dua objek", "application/json", `{"name":"a"}{"name":"b"}`, false, 400, ""},
		{"terlalu besar", "application/json", `{"name":"` + strings.Repeat("x", MaxBodyBytes) + `"}`, false, 413, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/x", strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			rr := httptest.NewRecorder()
			var in input
			ok := Decode(rr, req, &in)
			if ok != tc.wantOK {
				t.Fatalf("ok mau %v, dapat %v (%s)", tc.wantOK, ok, rr.Body.String())
			}
			if ok {
				if in.Name != "a" {
					t.Errorf("isi nggak terbaca: %+v", in)
				}
				return
			}
			p := decodeProblem(t, rr)
			if rr.Code != tc.wantStatus || p.Status != tc.wantStatus {
				t.Errorf("status mau %d, dapat %d", tc.wantStatus, rr.Code)
			}
			if tc.wantField != "" && (len(p.Errors) != 1 || p.Errors[0].Field != tc.wantField) {
				t.Errorf("errors harus menunjuk field %q: %+v", tc.wantField, p.Errors)
			}
		})
	}
}