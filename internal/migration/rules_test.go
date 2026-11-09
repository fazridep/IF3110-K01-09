package migration

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func mig(version int, status Status) Migration {
	return Migration{ID: uuid.New(), Version: version, Status: status,
		Definition: Definition{UpSQL: "SELECT 1;", DownSQL: "SELECT 1;", Mode: ModeNormal}}
}

func TestActionRuleUrutanLinear(t *testing.T) {
	a, b, c := mig(1, StatusSuccess), mig(2, StatusSuccess), mig(3, StatusSuccess)
	all := []Migration{a, b, c}

	if err := actionRule(&b, all, ActionRollback); !errors.Is(err, ErrActionNotAllowed) {
		t.Fatalf("rollback B sebelum C harus ditolak, dapat %v", err)
	}
	if err := actionRule(&c, all, ActionRollback); err != nil {
		t.Fatalf("rollback C harus boleh: %v", err)
	}

	// C dan B sudah di-rollback: forward harus mulai dari B.
	b.Status, c.Status = StatusRolledBack, StatusRolledBack
	all = []Migration{a, b, c}
	if err := actionRule(&c, all, ActionForward); !errors.Is(err, ErrActionNotAllowed) {
		t.Fatalf("forward C sebelum B harus ditolak, dapat %v", err)
	}
	if err := actionRule(&b, all, ActionForward); err != nil {
		t.Fatalf("forward B harus boleh: %v", err)
	}
	if err := actionRule(&a, all, ActionRollback); err != nil {
		t.Fatalf("A sekarang aktif terakhir, rollback harus boleh: %v", err)
	}
	if err := actionRule(&a, all, ActionForward); !errors.Is(err, ErrActionNotAllowed) {
		t.Fatalf("forward A yang masih aktif harus ditolak, dapat %v", err)
	}
}

func TestActionRuleLainnya(t *testing.T) {
	irr := mig(1, StatusSuccess)
	irr.Irreversible, irr.DownSQL, irr.IrreversibleReason = true, "", "hapus kolom"
	draft := mig(2, StatusDraft)
	partial := mig(1, StatusPartial)
	partial.Mode = ModeBatch
	failed := mig(1, StatusFailed)

	cases := []struct {
		name     string
		m        Migration
		siblings []Migration
		action   string
		ok       bool
	}{
		{"irreversible nggak bisa rollback", irr, []Migration{irr}, ActionRollback, false},
		{"draft bisa apply", draft, []Migration{irr, draft}, ActionApply, true},
		{"draft nggak bisa rollback", draft, []Migration{draft}, ActionRollback, false},
		{"apply ditahan selama ada partial", draft, []Migration{partial, draft}, ActionApply, false},
		{"partial bisa dilanjutkan", partial, []Migration{partial}, ActionResume, true},
		{"partial bisa dibatalkan", partial, []Migration{partial}, ActionRollback, true},
		{"normal nggak bisa dilanjutkan", mig(1, StatusPartial), nil, ActionResume, false},
		{"failed nggak bisa forward", failed, []Migration{failed}, ActionForward, false},
		{"aksi asing", draft, nil, "hapus", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := actionRule(&tc.m, tc.siblings, tc.action)
			if tc.ok != (err == nil) {
				t.Fatalf("mau ok=%v, dapat %v", tc.ok, err)
			}
			var na *NotAllowedError
			if err != nil && (!errors.As(err, &na) || na.Reason == "") {
				t.Fatalf("error harus NotAllowedError dengan alasan, dapat %v", err)
			}
		})
	}
}

// Contoh dari spesifikasi: A[E1] B[E2] B(rollback E2)[E3] B(forward)[E4] C[E5]
// jadi A[E1] B[E4] C[E5].
func TestHideRolledBackContohSpesifikasi(t *testing.T) {
	ids := make([]uuid.UUID, 6)
	for i := range ids {
		ids[i] = uuid.New()
	}
	e := func(n int, action string, status ExecStatus, reverts int) Execution {
		x := Execution{ID: ids[n], Action: action, Status: status}
		if reverts > 0 {
			x.RevertsExecutionID = &ids[reverts]
		}
		return x
	}
	all := []Execution{
		e(1, ActionApply, ExecSuccess, 0),
		e(2, ActionApply, ExecSuccess, 0),
		e(3, ActionRollback, ExecSuccess, 2),
		e(4, ActionForward, ExecSuccess, 0),
		e(5, ActionApply, ExecSuccess, 0),
	}
	got := hideRolledBack(all)
	want := []uuid.UUID{ids[1], ids[4], ids[5]}
	if len(got) != len(want) {
		t.Fatalf("mau %d entri, dapat %d", len(want), len(got))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("entri ke-%d salah", i)
		}
	}
	if len(all) != 5 {
		t.Fatal("filter nggak boleh mengubah data aslinya")
	}

	// Rollback gagal nggak membatalkan apa pun, jadi dua-duanya tetap terlihat.
	failed := []Execution{e(1, ActionApply, ExecSuccess, 0), e(2, ActionRollback, ExecFailed, 1)}
	if got := hideRolledBack(failed); len(got) != 2 {
		t.Fatalf("rollback gagal harus tetap terlihat, dapat %d entri", len(got))
	}
}

func TestValidate(t *testing.T) {
	batch := Definition{Description: "naikkan harga", Mode: ModeBatch,
		UpSQL:   "UPDATE orders o SET amount = o.amount * 2 FROM batch_rows b WHERE o.id = b.id;",
		DownSQL: "UPDATE orders o SET amount = b.amount FROM batch_rows b WHERE o.id = b.id;",
		Batch:   Batch{Table: "public.orders", Key: "id", Size: 1000}}

	cases := []struct {
		name  string
		d     Definition
		field string
	}{
		{"lengkap", normal("CREATE TABLE a (id INT);", "DROP TABLE a;"), ""},
		{"tanpa deskripsi", Definition{UpSQL: "SELECT 1;", DownSQL: "SELECT 1;"}, "description"},
		{"tanpa down", normal("CREATE TABLE a (id INT);", ""), "downSql"},
		{"irreversible tanpa alasan", Definition{Description: "x", UpSQL: "SELECT 1;", Irreversible: true}, "irreversibleReason"},
		{"irreversible dengan alasan", Definition{Description: "x", UpSQL: "DROP TABLE a;", Irreversible: true, IrreversibleReason: "data hilang"}, ""},
		{"down nggak transactional", normal("SELECT 1;", "VACUUM;"), "downSql"},
		{"up kosong", normal("  ", "SELECT 1;"), "upSql"},
		{"batch lengkap", batch, ""},
	}
	bad := batch
	bad.UpSQL = "UPDATE orders SET amount = amount * 2;"
	cases = append(cases, struct {
		name  string
		d     Definition
		field string
	}{"batch tanpa batch_rows", bad, "upSql"})
	bad = batch
	bad.Batch.Table = "public.orders; DROP TABLE x"
	cases = append(cases, struct {
		name  string
		d     Definition
		field string
	}{"nama tabel aneh", bad, "batch.table"})
	bad = batch
	bad.Batch.Size = 0
	cases = append(cases, struct {
		name  string
		d     Definition
		field string
	}{"ukuran batch nol", bad, "batch.size"})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validate(normalize(tc.d))
			if tc.field == "" {
				if err != nil {
					t.Fatalf("mau lolos, dapat %v", err)
				}
				return
			}
			var in *InputError
			if !errors.As(err, &in) || in.Field != tc.field {
				t.Fatalf("mau error di %s, dapat %v", tc.field, err)
			}
		})
	}
}

func TestUsesBatchRowsAbaikanKomentarDanString(t *testing.T) {
	if usesBatchRows("UPDATE t SET x = 'batch_rows'; -- batch_rows") {
		t.Fatal("batch_rows di string atau komentar nggak dihitung")
	}
	if !usesBatchRows("update t set x = 1 from BATCH_ROWS b where t.id = b.id") {
		t.Fatal("huruf besar kecil nggak berpengaruh")
	}
}
