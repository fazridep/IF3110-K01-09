package migration

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

func (s *Service) CanEdit(ctx context.Context, userID, databaseID uuid.UUID) (bool, error) {
	return s.perms.CanEdit(ctx, userID, databaseID)
}

func (s *Service) List(ctx context.Context, userID, databaseID uuid.UUID) ([]Migration, error) {
	if err := s.requireView(ctx, userID, databaseID); err != nil {
		return nil, err
	}
	return listByDatabase(ctx, s.db, databaseID)
}

func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (*Migration, error) {
	m, err := findByID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if err := s.requireView(ctx, userID, m.DatabaseID); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Service) Create(ctx context.Context, userID, databaseID uuid.UUID, d Definition) (*Migration, error) {
	if err := s.requireEdit(ctx, userID, databaseID); err != nil {
		return nil, err
	}
	d = normalize(d)
	if err := s.validateWithTarget(ctx, databaseID, d); err != nil {
		return nil, err
	}
	return s.insertDraft(ctx, databaseID, userID, d)
}

// Update cuma buat draft. SQL migration yang sudah pernah dieksekusi immutable,
// perbaikannya dibuat sebagai migration baru.
func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, d Definition) (*Migration, error) {
	m, err := findByID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if err := s.requireEdit(ctx, userID, m.DatabaseID); err != nil {
		return nil, err
	}
	if m.Status != StatusDraft {
		return nil, ErrNotDraft
	}
	d = normalize(d)
	if err := s.validateWithTarget(ctx, m.DatabaseID, d); err != nil {
		return nil, err
	}
	if err := s.updateDraft(ctx, id, d); err != nil {
		return nil, err
	}
	return findByID(ctx, s.db, id)
}

// validateWithTarget menambah cek tabel dan kolom kursor di database target
// buat mode batch, supaya salah ketik ketahuan sebelum masuk approval.
func (s *Service) validateWithTarget(ctx context.Context, databaseID uuid.UUID, d Definition) error {
	if err := validate(d); err != nil {
		return err
	}
	if d.Mode != ModeBatch {
		return nil
	}
	pool, err := s.targets.Pool(ctx, databaseID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTargetUnavailable, err)
	}
	_, err = resolveBatchTarget(ctx, pool, d.Batch)
	if errors.Is(err, ErrInvalidBatch) {
		return invalid("batch.table", err)
	}
	return err
}

// CheckAction dipanggil package approval sebelum permintaan dibuat, supaya
// aksi yang pasti ditolak worker nggak perlu sampai ke leader.
func (s *Service) CheckAction(ctx context.Context, migrationID uuid.UUID, action string) error {
	m, err := findByID(ctx, s.db, migrationID)
	if err != nil {
		return err
	}
	return checkAction(ctx, s.db, m, action)
}

// AllowedActions aksi yang saat ini boleh diajukan untuk migration m.
func (s *Service) AllowedActions(ctx context.Context, m *Migration) ([]string, error) {
	siblings, err := listByDatabase(ctx, s.db, m.DatabaseID)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, a := range Actions {
		if actionRule(m, siblings, a) == nil {
			out = append(out, a)
		}
	}
	return out, nil
}

// History riwayat eksekusi satu database, terlama dulu. Eksekusi yang gagal
// tetap muncul. Dengan HideRollback, pasangan eksekusi yang dibatalkan dan
// rollback sukses yang membatalkannya disembunyikan.
func (s *Service) History(ctx context.Context, userID, databaseID uuid.UUID, f HistoryFilter) ([]Execution, error) {
	if err := s.requireView(ctx, userID, databaseID); err != nil {
		return nil, err
	}
	list, err := listExecutions(ctx, s.db, databaseID, f)
	if err != nil {
		return nil, err
	}
	if f.HideRollback {
		list = hideRolledBack(list)
	}
	return list, nil
}

// hideRolledBack memasangkan rollback dengan eksekusi yang dibatalkannya lewat
// execution ID, bukan nama migration. Rollback yang gagal nggak membatalkan apa
// pun, jadi tetap terlihat.
func hideRolledBack(list []Execution) []Execution {
	hidden := map[uuid.UUID]bool{}
	for _, e := range list {
		if e.Action == ActionRollback && e.Status == ExecSuccess && e.RevertsExecutionID != nil {
			hidden[e.ID] = true
			hidden[*e.RevertsExecutionID] = true
		}
	}
	out := make([]Execution, 0, len(list))
	for _, e := range list {
		if !hidden[e.ID] {
			out = append(out, e)
		}
	}
	return out
}

func checkAction(ctx context.Context, q querier, m *Migration, action string) error {
	siblings, err := listByDatabase(ctx, q, m.DatabaseID)
	if err != nil {
		return err
	}
	return actionRule(m, siblings, action)
}

// actionRule aturan urutan linear (ADR-015). siblings semua migration di
// database yang sama, termasuk m.
func actionRule(m *Migration, siblings []Migration, action string) error {
	// top versi tertinggi yang perubahannya sedang ada di database.
	top := 0
	for _, o := range siblings {
		if o.Status.applied() && o.Version > top {
			top = o.Version
		}
	}

	switch action {
	case ActionApply:
		if m.Status != StatusDraft {
			return notAllowed("migration ini sudah pernah dijalankan, apply cuma buat draft")
		}
		for _, o := range siblings {
			if o.ID != m.ID && (o.Status == StatusPartial || o.Status == StatusPartiallyReverted || o.Status == StatusRunning) {
				return notAllowed(fmt.Sprintf("v%d belum tuntas (%s), selesaikan dulu sebelum menjalankan migration baru", o.Version, o.Status))
			}
		}
		return nil

	case ActionForward:
		if m.Status != StatusRolledBack {
			return notAllowed("forward cuma buat migration yang sudah di-rollback")
		}
		if top > m.Version {
			return notAllowed(fmt.Sprintf("v%d masih aktif di atasnya, forward harus mengikuti urutan", top))
		}
		for _, o := range siblings {
			if o.Status == StatusRolledBack && o.Version > top && o.Version < m.Version {
				return notAllowed(fmt.Sprintf("forward harus urut, jalankan forward v%d dulu", o.Version))
			}
		}
		return nil

	case ActionRollback:
		if m.Irreversible || m.DownSQL == "" {
			reason := m.IrreversibleReason
			if reason == "" {
				reason = "nggak punya SQL down"
			}
			return notAllowed("migration ini irreversible: " + reason + ". Buat migration perbaikan baru")
		}
		if m.Status != StatusSuccess && m.Status != StatusPartial && m.Status != StatusPartiallyReverted {
			return notAllowed("cuma migration yang sudah berhasil dijalankan yang bisa di-rollback")
		}
		if top > m.Version {
			return notAllowed(fmt.Sprintf("rollback harus dari migration aktif terakhir, rollback v%d dulu", top))
		}
		return nil

	case ActionResume:
		if m.Mode != ModeBatch || m.Status != StatusPartial {
			return notAllowed("lanjutkan batch cuma buat migration batch yang statusnya partial")
		}
		return nil
	}
	return notAllowed("aksi " + action + " nggak dikenal")
}

// requireView menjawab ErrNotFound kalau user nggak boleh melihat database-nya,
// supaya keberadaan migration nggak bocor.
func (s *Service) requireView(ctx context.Context, userID, databaseID uuid.UUID) error {
	err := s.require(ctx, s.perms.CanView, userID, databaseID)
	if errors.Is(err, ErrForbidden) {
		return ErrNotFound
	}
	return err
}

// requireEdit: nggak bisa lihat jadi ErrNotFound, bisa lihat tapi bukan
// view_edit jadi ErrForbidden.
func (s *Service) requireEdit(ctx context.Context, userID, databaseID uuid.UUID) error {
	if err := s.requireView(ctx, userID, databaseID); err != nil {
		return err
	}
	return s.require(ctx, s.perms.CanEdit, userID, databaseID)
}

func (s *Service) require(ctx context.Context, check func(context.Context, uuid.UUID, uuid.UUID) (bool, error), userID, databaseID uuid.UUID) error {
	ok, err := check(ctx, userID, databaseID)
	if err != nil {
		return fmt.Errorf("migration: cek akses: %w", err)
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}
