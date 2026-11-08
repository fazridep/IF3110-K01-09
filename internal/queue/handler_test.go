package queue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/if3110/tubes-db-platform/internal/auth"
)

type fakePerms struct {
	view, edit bool
	databases  []uuid.UUID
}

func (p fakePerms) CanView(context.Context, uuid.UUID, uuid.UUID) (bool, error) { return p.view, nil }
func (p fakePerms) CanEdit(context.Context, uuid.UUID, uuid.UUID) (bool, error) { return p.edit, nil }
func (p fakePerms) ViewableDatabases(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return p.databases, nil
}

type fakeApprovals map[uuid.UUID]Approval

func (a fakeApprovals) Lookup(_ context.Context, _, id uuid.UUID) (Approval, error) {
	got, ok := a[id]
	if !ok {
		return Approval{}, ErrNotFound
	}
	return got, nil
}

func newAPI(f *fixture, perms fakePerms, approvals fakeApprovals) *http.ServeMux {
	mux := http.NewServeMux()
	NewHandler(NewService(f.store, perms, approvals, 3)).RegisterAPIRoutes(mux)
	return mux
}

func call(mux *http.ServeMux, f *fixture, method, target, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	r = r.WithContext(auth.WithUser(r.Context(), &auth.User{ID: f.user, Username: "pemilik"}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func (f *fixture) approved(t *testing.T, dbID uuid.UUID, action string, approved bool) fakeApprovals {
	t.Helper()
	in := f.approval(t, dbID, action)
	return fakeApprovals{in.ApprovalID: {in.ApprovalID, in.MigrationID, in.DatabaseID, action, approved}}
}

func firstID(a fakeApprovals) uuid.UUID {
	for id := range a {
		return id
	}
	return uuid.Nil
}

func TestAPISubmitMengembalikan202DanIdempoten(t *testing.T) {
	f := newFixture(t)
	dbID := f.database(t)
	approvals := f.approved(t, dbID, "apply", true)
	mux := newAPI(f, fakePerms{view: true, edit: true}, approvals)
	body := `{"approvalId":"` + firstID(approvals).String() + `"}`

	rec := call(mux, f, "POST", "/api/jobs", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("mau 202, dapat %d: %s", rec.Code, rec.Body.String())
	}
	var first struct{ JobID, Status, StatusURL string }
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.JobID == "" || first.Status != "queued" || first.StatusURL != "/api/jobs/"+first.JobID {
		t.Fatalf("response salah: %+v", first)
	}
	if rec.Header().Get("Location") != first.StatusURL {
		t.Fatalf("Location salah: %q", rec.Header().Get("Location"))
	}

	rec = call(mux, f, "POST", "/api/jobs", body)
	var second struct{ JobID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &second)
	if rec.Code != http.StatusAccepted || second.JobID != first.JobID {
		t.Fatalf("kirim ulang harus job yang sama: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAPISubmitDitolak(t *testing.T) {
	f := newFixture(t)
	dbID := f.database(t)
	ok := f.approved(t, dbID, "apply", true)
	pending := f.approved(t, dbID, "apply", false)

	cases := []struct {
		name  string
		perms fakePerms
		apps  fakeApprovals
		body  string
		code  int
	}{
		{"view only", fakePerms{view: true}, ok, `{"approvalId":"` + firstID(ok).String() + `"}`, http.StatusForbidden},
		{"belum disetujui", fakePerms{view: true, edit: true}, pending, `{"approvalId":"` + firstID(pending).String() + `"}`, http.StatusConflict},
		{"approval nggak ada", fakePerms{view: true, edit: true}, ok, `{"approvalId":"` + uuid.NewString() + `"}`, http.StatusNotFound},
		{"id salah", fakePerms{view: true, edit: true}, ok, `{"approvalId":"bukan"}`, http.StatusBadRequest},
		{"lanjut pakai approval bukan batch", fakePerms{view: true, edit: true}, ok, `{"approvalId":"` + firstID(ok).String() + `","resumeJobId":"` + uuid.NewString() + `"}`, http.StatusConflict},
	}
	for _, tc := range cases {
		rec := call(newAPI(f, tc.perms, tc.apps), f, "POST", "/api/jobs", tc.body)
		if rec.Code != tc.code {
			t.Errorf("%s: mau %d, dapat %d: %s", tc.name, tc.code, rec.Code, rec.Body.String())
		}
	}
	var n int
	if err := f.db.QueryRow(context.Background(), `SELECT count(*) FROM migration_jobs`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("nggak boleh ada job yang masuk: %d %v", n, err)
	}
}

func TestAPIDetailDanDaftarIkutAkses(t *testing.T) {
	f := newFixture(t)
	dbA, dbB := f.database(t), f.database(t)
	a := f.enqueue(t, dbA)
	b := f.enqueue(t, dbB)
	mux := newAPI(f, fakePerms{view: true, databases: []uuid.UUID{dbA}}, nil)

	rec := call(mux, f, "GET", "/api/jobs", "")
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != a.String() {
		t.Fatalf("cuma job database A: %s", rec.Body.String())
	}

	rec = call(mux, f, "GET", "/api/jobs?databaseId="+dbB.String(), "")
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("filter database tanpa akses harus kosong: %s", rec.Body.String())
	}

	if rec := call(mux, f, "GET", "/api/jobs/"+a.String(), ""); rec.Code != http.StatusOK {
		t.Fatalf("detail: mau 200, dapat %d", rec.Code)
	}

	denied := newAPI(f, fakePerms{}, nil)
	if rec := call(denied, f, "GET", "/api/jobs/"+b.String(), ""); rec.Code != http.StatusNotFound {
		t.Fatalf("tanpa akses harus 404, dapat %d", rec.Code)
	}
	if rec := call(denied, f, "GET", "/api/jobs", ""); !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("tanpa database harus kosong: %s", rec.Body.String())
	}
	if rec := call(denied, f, "GET", "/api/jobs/bukan-uuid", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("id salah harus 404, dapat %d", rec.Code)
	}
}

func TestAPIRecheck(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	id := f.enqueue(t, f.database(t))
	path := "/api/jobs/" + id.String() + "/recheck"

	if rec := call(newAPI(f, fakePerms{view: true}, nil), f, "POST", path, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("view only: mau 403, dapat %d", rec.Code)
	}
	editor := newAPI(f, fakePerms{view: true, edit: true}, nil)
	if rec := call(editor, f, "POST", path, ""); rec.Code != http.StatusConflict {
		t.Fatalf("job queued: mau 409, dapat %d", rec.Code)
	}
	if _, err := f.db.Exec(ctx, `UPDATE migration_jobs SET status='uncertain', verify_attempts=max_attempts WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if rec := call(editor, f, "POST", path, ""); rec.Code != http.StatusOK {
		t.Fatalf("job uncertain: mau 200, dapat %d", rec.Code)
	}
	if got := f.status(t, id).VerifyAttempts; got != 0 {
		t.Fatalf("verify_attempts harus direset, dapat %d", got)
	}
}
