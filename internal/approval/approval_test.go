package approval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/auth"
	"github.com/if3110/tubes-db-platform/internal/httpx"
	"github.com/if3110/tubes-db-platform/internal/testutil"
)

func seedUser(t *testing.T, db *pgxpool.Pool, username string) *auth.User {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(context.Background(),
		`INSERT INTO users (username, password_hash) VALUES ($1, 'x') RETURNING id`, username,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return &auth.User{ID: id, Username: username}
}

func seedGroupWithLeader(t *testing.T, db *pgxpool.Pool, name string, leader *auth.User, members ...*auth.User) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var groupID uuid.UUID
	if err := db.QueryRow(ctx, `INSERT INTO groups (name) VALUES ($1) RETURNING id`, name).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	all := append([]*auth.User{leader}, members...)
	for _, m := range all {
		if m == nil {
			continue
		}
		if _, err := db.Exec(ctx, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, groupID, m.ID); err != nil {
			t.Fatal(err)
		}
	}
	if leader != nil {
		if _, err := db.Exec(ctx, `UPDATE groups SET leader_id = $1 WHERE id = $2`, leader.ID, groupID); err != nil {
			t.Fatal(err)
		}
	}
	return groupID
}

func seedDatabase(t *testing.T, db *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(context.Background(), `
		INSERT INTO database_connections (name, host, db_name, username, password_enc)
		VALUES ($1, 'localhost', $1, 'app', '\x00') RETURNING id`, name,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedGrant(t *testing.T, db *pgxpool.Pool, groupID, dbID uuid.UUID, level string) {
	t.Helper()
	if _, err := db.Exec(context.Background(),
		`INSERT INTO group_database_access (group_id, database_id, level) VALUES ($1, $2, $3)`,
		groupID, dbID, level,
	); err != nil {
		t.Fatal(err)
	}
}

func seedMigration(t *testing.T, db *pgxpool.Pool, dbID, createdBy uuid.UUID, version int, desc, sqlText string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(context.Background(), `
		INSERT INTO database_migrations (database_id, version, description, sql_text, status, created_by)
		VALUES ($1, $2, $3, $4, 'draft', $5) RETURNING id`,
		dbID, version, desc, sqlText, createdBy,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func doReq(mux *http.ServeMux, method, target, body string, u *auth.User) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	if u != nil {
		r = r.WithContext(auth.WithUser(r.Context(), u))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func TestApprovalRequiresAllDatabaseGroupLeaders(t *testing.T) {
	db := testutil.DB(t)
	perms := access.NewStore(db)
	svc := NewService(db, perms)

	approvedHookCalled := 0
	svc.SetOnApproved(func(ctx context.Context, req *Request) error {
		approvedHookCalled++
		return nil
	})

	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.RegisterAPIRoutes(mux)

	leaderBackend := seedUser(t, db, "leader_backend")
	editorBackend := seedUser(t, db, "editor_backend")
	leaderAnalytics := seedUser(t, db, "leader_analytics")
	stranger := seedUser(t, db, "stranger")

	dbID := seedDatabase(t, db, "prod_orders")
	gBackend := seedGroupWithLeader(t, db, "backend", leaderBackend, editorBackend)
	gAnalytics := seedGroupWithLeader(t, db, "analytics", leaderAnalytics)

	// Backend punya view_edit, Analytics punya view_only ke database prod_orders
	seedGrant(t, db, gBackend, dbID, "view_edit")
	seedGrant(t, db, gAnalytics, dbID, "view_only")

	migID := seedMigration(t, db, dbID, editorBackend.ID, 1, "create orders table", "CREATE TABLE orders (id INT PRIMARY KEY);")

	// 1. User view_only tidak boleh submit approval
	rec := doReq(mux, http.MethodPost, "/api/approvals", `{"migrationId":"`+migID.String()+`"}`, leaderAnalytics)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("view_only submit harus 403, dapat %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Editor (view_edit) submit approval -> 201 Created dengan status pending_review dan 2 group wajib approve
	rec = doReq(mux, http.MethodPost, "/api/approvals", `{"migrationId":"`+migID.String()+`","action":"apply"}`, editorBackend)
	if rec.Code != http.StatusCreated {
		t.Fatalf("editor submit harus 201, dapat %d: %s", rec.Code, rec.Body.String())
	}
	var created approvalJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != StatusPendingReview || created.TotalRequired != 2 || created.ApprovedCount != 0 {
		t.Fatalf("status awal salah: %+v", created)
	}

	// 3. Stranger (tanpa akses ke db) coba GET -> 404 (cegah BOLA)
	rec = doReq(mux, http.MethodGet, "/api/approvals/"+created.ID.String(), "", stranger)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger GET harus 404, dapat %d", rec.Code)
	}

	// 4. Editor biasa (bukan leader) coba approve -> 403 Forbidden
	rec = doReq(mux, http.MethodPost, "/api/approvals/"+created.ID.String()+"/approve", `{"note":"saya setuju"}`, editorBackend)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bukan leader approve harus 403, dapat %d", rec.Code)
	}

	// 5. Leader Backend approve -> baru 1 dari 2 group, status HARUS tetap pending_review!
	rec = doReq(mux, http.MethodPost, "/api/approvals/"+created.ID.String()+"/approve", `{"note":"LGTM dari backend"}`, leaderBackend)
	if rec.Code != http.StatusOK {
		t.Fatalf("leaderBackend approve harus 200, dapat %d: %s", rec.Code, rec.Body.String())
	}
	var afterFirst approvalJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &afterFirst)
	if afterFirst.Status != StatusPendingReview || afterFirst.ApprovedCount != 1 || afterFirst.TotalRequired != 2 {
		t.Fatalf("setelah 1 leader approve, status harus masih pending_review (1/2), dapat: %+v", afterFirst)
	}
	if approvedHookCalled != 0 {
		t.Fatalf("hook onApproved belum boleh terpanggil sebelum semua leader approve")
	}

	// 6. Leader Backend coba approve lagi -> 409 Conflict
	rec = doReq(mux, http.MethodPost, "/api/approvals/"+created.ID.String()+"/approve", `{}`, leaderBackend)
	if rec.Code != http.StatusConflict {
		t.Fatalf("approve dua kali oleh leader yang sama harus 409, dapat %d", rec.Code)
	}

	// 7. Leader Analytics (group view_only) approve -> sekarang 2/2 leader sudah approve, status berubah jadi approved!
	rec = doReq(mux, http.MethodPost, "/api/approvals/"+created.ID.String()+"/approve", `{"note":"Aman untuk pelaporan"}`, leaderAnalytics)
	if rec.Code != http.StatusOK {
		t.Fatalf("leaderAnalytics approve harus 200, dapat %d: %s", rec.Code, rec.Body.String())
	}
	var afterSecond approvalJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &afterSecond)
	if afterSecond.Status != StatusApproved || afterSecond.ApprovedCount != 2 || afterSecond.TotalRequired != 2 || afterSecond.ResolvedAt == nil {
		t.Fatalf("setelah semua leader approve, status harus approved (2/2), dapat: %+v", afterSecond)
	}
	if approvedHookCalled != 1 {
		t.Fatalf("hook onApproved harus terpanggil tepat sekali, dapat %d", approvedHookCalled)
	}
}

func TestApprovalRejectFlow(t *testing.T) {
	db := testutil.DB(t)
	perms := access.NewStore(db)
	svc := NewService(db, perms)
	h := NewHandler(svc)
	mux := http.NewServeMux()
	h.RegisterAPIRoutes(mux)

	leader1 := seedUser(t, db, "leader1")
	leader2 := seedUser(t, db, "leader2")
	dbID := seedDatabase(t, db, "inventory")
	g1 := seedGroupWithLeader(t, db, "g1", leader1)
	g2 := seedGroupWithLeader(t, db, "g2", leader2)
	seedGrant(t, db, g1, dbID, "view_edit")
	seedGrant(t, db, g2, dbID, "view_only")

	migID := seedMigration(t, db, dbID, leader1.ID, 1, "drop column", "ALTER TABLE items DROP COLUMN legacy;")

	rec := doReq(mux, http.MethodPost, "/api/approvals", `{"migrationId":"`+migID.String()+`"}`, leader1)
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit harus 201, dapat %d", rec.Code)
	}
	var created approvalJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Reject tanpa alasan -> 400 dengan field error "reason"
	rec = doReq(mux, http.MethodPost, "/api/approvals/"+created.ID.String()+"/reject", `{"reason":"  "}`, leader2)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reject tanpa alasan harus 400, dapat %d", rec.Code)
	}
	var prob httpx.Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &prob)
	if len(prob.Errors) == 0 || prob.Errors[0].Field != "reason" {
		t.Fatalf("harus ada field error reason: %+v", prob)
	}

	// Leader 2 menolak -> langsung berstatus rejected
	rec = doReq(mux, http.MethodPost, "/api/approvals/"+created.ID.String()+"/reject", `{"reason":"Kolom legacy masih dipakai laporan bulanan"}`, leader2)
	if rec.Code != http.StatusOK {
		t.Fatalf("reject harus 200, dapat %d: %s", rec.Code, rec.Body.String())
	}
	var rejected approvalJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &rejected)
	if rejected.Status != StatusRejected || rejected.RejectionReason == "" {
		t.Fatalf("status harus rejected dengan alasan: %+v", rejected)
	}

	// Leader 1 tidak bisa approve request yang sudah rejected -> 409 Conflict
	rec = doReq(mux, http.MethodPost, "/api/approvals/"+created.ID.String()+"/approve", `{}`, leader1)
	if rec.Code != http.StatusConflict {
		t.Fatalf("approve setelah rejected harus 409, dapat %d", rec.Code)
	}
}

