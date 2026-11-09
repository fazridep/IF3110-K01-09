package migration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/auth"
)

type api struct {
	t   *testing.T
	mux *http.ServeMux
}

func newAPI(t *testing.T, f *fixture) *api {
	mux := http.NewServeMux()
	NewHandler(f.svc).RegisterAPIRoutes(mux)
	return &api{t: t, mux: mux}
}

func (a *api) do(userID uuid.UUID, method, path, body string) *httptest.ResponseRecorder {
	a.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req = req.WithContext(auth.WithUser(req.Context(), &auth.User{ID: userID}))
	rr := httptest.NewRecorder()
	a.mux.ServeHTTP(rr, req)
	return rr
}

func decode[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("body bukan JSON: %v\n%s", err, rr.Body.String())
	}
	return v
}

func TestAPIAlurMigration(t *testing.T) {
	f := newFixture(t)
	a := newAPI(t, f)
	base := "/api/databases/" + f.dbID.String()

	rr := a.do(f.editor, http.MethodPost, base+"/migrations", `{"description":"tabel produk","upSql":"CREATE TABLE produk (id INT);"}`)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), `"field":"downSql"`) {
		t.Fatalf("tanpa SQL down mau 400 di downSql, dapat %d %s", rr.Code, rr.Body.String())
	}
	rr = a.do(f.viewer, http.MethodPost, base+"/migrations", `{"description":"x","upSql":"SELECT 1;","downSql":"SELECT 1;"}`)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("viewer mau 403, dapat %d", rr.Code)
	}

	rr = a.do(f.editor, http.MethodPost, base+"/migrations",
		`{"description":"tabel produk","upSql":"CREATE TABLE produk (id INT);","downSql":"DROP TABLE produk;"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create mau 201, dapat %d %s", rr.Code, rr.Body.String())
	}
	created := decode[migrationJSON](t, rr)
	if created.Version != 1 || created.Status != StatusDraft || created.DownSQL != "DROP TABLE produk;" || created.Batch != nil {
		t.Fatalf("hasil create salah: %+v", created)
	}
	if rr.Header().Get("Location") != "/api/migrations/"+created.ID.String() {
		t.Fatalf("Location salah: %s", rr.Header().Get("Location"))
	}

	rr = a.do(f.editor, http.MethodPut, "/api/migrations/"+created.ID.String(),
		`{"description":"tabel produk v2","upSql":"CREATE TABLE produk (id INT, nama TEXT);","downSql":"DROP TABLE produk;"}`)
	if rr.Code != http.StatusOK || decode[migrationJSON](t, rr).Description != "tabel produk v2" {
		t.Fatalf("update mau 200, dapat %d %s", rr.Code, rr.Body.String())
	}

	rr = a.do(f.viewer, http.MethodGet, "/api/migrations/"+created.ID.String(), "")
	detail := decode[migrationDetailJSON](t, rr)
	if rr.Code != http.StatusOK || detail.CanEdit || len(detail.AllowedActions) != 1 || detail.AllowedActions[0] != ActionApply {
		t.Fatalf("detail viewer salah: %d %+v", rr.Code, detail)
	}
	if rr := a.do(f.stranger, http.MethodGet, "/api/migrations/"+created.ID.String(), ""); rr.Code != http.StatusNotFound {
		t.Fatalf("stranger mau 404, dapat %d", rr.Code)
	}
	if rr := a.do(f.stranger, http.MethodGet, base+"/migrations", ""); rr.Code != http.StatusNotFound {
		t.Fatalf("stranger list mau 404, dapat %d", rr.Code)
	}

	m := f.reload(&Migration{ID: created.ID})
	f.mustRun(m, ActionApply)
	f.mustRun(m, ActionRollback)
	rr = a.do(f.editor, http.MethodPut, "/api/migrations/"+created.ID.String(),
		`{"description":"x","upSql":"SELECT 1;","downSql":"SELECT 1;"}`)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "migration-immutable") {
		t.Fatalf("ubah setelah dijalankan mau 409, dapat %d %s", rr.Code, rr.Body.String())
	}

	rr = a.do(f.viewer, http.MethodGet, base+"/history", "")
	full := decode[struct{ Items []executionJSON }](t, rr)
	if rr.Code != http.StatusOK || len(full.Items) != 2 || full.Items[1].RevertsExecutionID == nil {
		t.Fatalf("history mau 2 eksekusi, dapat %d %+v", rr.Code, full)
	}
	rr = a.do(f.viewer, http.MethodGet, base+"/history?hideRollback=true", "")
	if hidden := decode[struct{ Items []executionJSON }](t, rr); len(hidden.Items) != 0 {
		t.Fatalf("hide rollback mau kosong, dapat %+v", hidden)
	}
}

func TestAPIBatch(t *testing.T) {
	f := newFixture(t)
	f.seedOrders()
	a := newAPI(t, f)
	body := `{"description":"naikkan amount","mode":"batch",
		"upSql":"UPDATE orders o SET amount = o.amount * 2 FROM batch_rows b WHERE o.id = b.id;",
		"downSql":"UPDATE orders o SET amount = b.amount FROM batch_rows b WHERE o.id = b.id;",
		"batch":{"table":"orders","key":"id","size":0,"backup":true}}`
	rr := a.do(f.editor, http.MethodPost, "/api/databases/"+f.dbID.String()+"/migrations", body)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), `"field":"batch.size"`) {
		t.Fatalf("ukuran batch 0 mau 400, dapat %d %s", rr.Code, rr.Body.String())
	}
	rr = a.do(f.editor, http.MethodPost, "/api/databases/"+f.dbID.String()+"/migrations", strings.Replace(body, `"size":0`, `"size":500`, 1))
	if rr.Code != http.StatusCreated {
		t.Fatalf("mau 201, dapat %d %s", rr.Code, rr.Body.String())
	}
	got := decode[migrationJSON](t, rr)
	if got.Mode != ModeBatch || got.Batch == nil || got.Batch.Size != 500 || !got.Batch.Backup {
		t.Fatalf("konfigurasi batch salah: %+v", got)
	}
}
