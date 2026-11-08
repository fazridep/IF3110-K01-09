package approval

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/if3110/tubes-db-platform/internal/access"
	"github.com/if3110/tubes-db-platform/internal/platform/pgerr"
)

func (s *Service) Submit(ctx context.Context, userID, migrationID uuid.UUID, action Action) (*Request, error) {
	if action == "" {
		action = ActionApply
	}
	if !action.Valid() {
		return nil, ErrInvalidAction
	}

	var dbID uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT database_id FROM database_migrations WHERE id = $1`, migrationID).Scan(&dbID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("approval: cari migration: %w", err)
	}

	canEdit, err := s.perms.CanEdit(ctx, userID, dbID)
	if err != nil {
		return nil, fmt.Errorf("approval: cek akses edit: %w", err)
	}
	if !canEdit {
		return nil, ErrForbidden
	}

	groups, err := s.perms.DatabaseApproverGroups(ctx, dbID)
	if err != nil {
		return nil, fmt.Errorf("approval: ambil approver groups: %w", err)
	}
	if len(groups) == 0 {
		return nil, ErrNoApproverGroups
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("approval: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var reqID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO approval_requests (migration_id, database_id, action, status, requested_by)
		VALUES ($1, $2, $3, 'pending_review', $4)
		RETURNING id`,
		migrationID, dbID, string(action), userID,
	).Scan(&reqID)
	if pgerr.IsUniqueViolation(err) {
		return nil, ErrPendingExists
	}
	if err != nil {
		return nil, fmt.Errorf("approval: buat request: %w", err)
	}

	for _, g := range groups {
		if _, err := tx.Exec(ctx, `
			INSERT INTO approval_group_decisions (request_id, group_id, decision)
			VALUES ($1, $2, 'pending_review')
			ON CONFLICT (request_id, group_id) DO NOTHING`,
			reqID, g.GroupID,
		); err != nil {
			return nil, fmt.Errorf("approval: buat group decision: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("approval: commit submit: %w", err)
	}
	return s.Get(ctx, userID, reqID)
}

// List mengembalikan daftar permintaan approval untuk database yang boleh diakses user.
func (s *Service) List(ctx context.Context, userID uuid.UUID, statusFilter string, dbIDFilter *uuid.UUID) ([]Request, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			ar.id,
			ar.migration_id,
			ar.database_id,
			dc.name,
			dm.version,
			dm.description,
			dm.sql_text,
			ar.action,
			ar.status,
			ar.requested_by,
			u.username,
			ar.rejection_reason,
			ar.created_at,
			ar.updated_at,
			ar.resolved_at
		FROM approval_requests ar
		JOIN database_connections dc ON dc.id = ar.database_id
		JOIN database_migrations dm ON dm.id = ar.migration_id
		JOIN users u ON u.id = ar.requested_by
		WHERE EXISTS (
			SELECT 1
			FROM group_database_access gda
			JOIN group_members gm ON gm.group_id = gda.group_id
			WHERE gda.database_id = ar.database_id AND gm.user_id = $1
		)
		AND ($2 = '' OR $2 = 'all' OR ar.status = $2)
		AND ($3::uuid IS NULL OR ar.database_id = $3)
		ORDER BY
			CASE ar.status WHEN 'pending_review' THEN 0 ELSE 1 END,
			ar.created_at DESC`,
		userID, strings.TrimSpace(statusFilter), dbIDFilter,
	)
	if err != nil {
		return nil, fmt.Errorf("approval: list requests: %w", err)
	}
	defer rows.Close()

	var items []Request
	for rows.Next() {
		var r Request
		var act, st string
		if err := rows.Scan(
			&r.ID,
			&r.MigrationID,
			&r.DatabaseID,
			&r.DatabaseName,
			&r.Version,
			&r.Description,
			&r.SQL,
			&act,
			&st,
			&r.RequestedByID,
			&r.RequestedBy,
			&r.RejectionReason,
			&r.CreatedAt,
			&r.UpdatedAt,
			&r.ResolvedAt,
		); err != nil {
			return nil, fmt.Errorf("approval: scan request: %w", err)
		}
		r.Action = Action(act)
		r.Status = Status(st)
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range items {
		if err := s.populateDecisions(ctx, userID, &items[i]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, userID, requestID uuid.UUID) (*Request, error) {
	var r Request
	var act, st string
	err := s.db.QueryRow(ctx, `
		SELECT
			ar.id,
			ar.migration_id,
			ar.database_id,
			dc.name,
			dm.version,
			dm.description,
			dm.sql_text,
			ar.action,
			ar.status,
			ar.requested_by,
			u.username,
			ar.rejection_reason,
			ar.created_at,
			ar.updated_at,
			ar.resolved_at
		FROM approval_requests ar
		JOIN database_connections dc ON dc.id = ar.database_id
		JOIN database_migrations dm ON dm.id = ar.migration_id
		JOIN users u ON u.id = ar.requested_by
		WHERE ar.id = $1`,
		requestID,
	).Scan(
		&r.ID,
		&r.MigrationID,
		&r.DatabaseID,
		&r.DatabaseName,
		&r.Version,
		&r.Description,
		&r.SQL,
		&act,
		&st,
		&r.RequestedByID,
		&r.RequestedBy,
		&r.RejectionReason,
		&r.CreatedAt,
		&r.UpdatedAt,
		&r.ResolvedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("approval: get request: %w", err)
	}
	r.Action = Action(act)
	r.Status = Status(st)

	canView, err := s.perms.CanView(ctx, userID, r.DatabaseID)
	if err != nil {
		return nil, fmt.Errorf("approval: cek akses view: %w", err)
	}
	if !canView {
		// Kembalikan ErrNotFound agar keberadaan resource tidak bocor (cegah BOLA).
		return nil, ErrNotFound
	}

	if err := s.populateDecisions(ctx, userID, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Service) populateDecisions(ctx context.Context, userID uuid.UUID, r *Request) error {
	if r.Status == StatusPendingReview {
		if _, err := s.db.Exec(ctx, `
			INSERT INTO approval_group_decisions (request_id, group_id, decision)
			SELECT $1, gda.group_id, 'pending_review'
			FROM group_database_access gda
			WHERE gda.database_id = $2
			ON CONFLICT (request_id, group_id) DO NOTHING`,
			r.ID, r.DatabaseID,
		); err != nil {
			return fmt.Errorf("approval: sync group decisions: %w", err)
		}
	}

	rows, err := s.db.Query(ctx, `
		SELECT
			g.id,
			g.name,
			COALESCE(gda.level, 'view_only'),
			l.id,
			COALESCE(l.username, ''),
			agd.decision,
			COALESCE(du.username, ''),
			agd.note,
			agd.decided_at
		FROM approval_group_decisions agd
		JOIN groups g ON g.id = agd.group_id
		LEFT JOIN group_database_access gda ON gda.group_id = g.id AND gda.database_id = $2
		LEFT JOIN group_members lgm ON lgm.group_id = g.id AND lgm.user_id = g.leader_id
		LEFT JOIN users l ON l.id = lgm.user_id AND l.disabled_at IS NULL
		LEFT JOIN users du ON du.id = agd.decided_by
		WHERE agd.request_id = $1
		  AND ($3 != 'pending_review' OR gda.group_id IS NOT NULL)
		ORDER BY g.name`,
		r.ID, r.DatabaseID, string(r.Status),
	)
	if err != nil {
		return fmt.Errorf("approval: query decisions: %w", err)
	}
	defer rows.Close()

	r.Approvals = nil
	r.ApprovedCount = 0
	r.TotalRequired = 0
	r.CanDecide = false
	r.MyLeaderGroups = nil

	for rows.Next() {
		var gd GroupDecision
		var lvl, dec string
		if err := rows.Scan(
			&gd.GroupID,
			&gd.GroupName,
			&lvl,
			&gd.LeaderID,
			&gd.LeaderUsername,
			&dec,
			&gd.DecidedBy,
			&gd.Note,
			&gd.DecidedAt,
		); err != nil {
			return fmt.Errorf("approval: scan decision: %w", err)
		}
		gd.AccessLevel = access.Level(lvl)
		gd.Decision = Status(dec)
		r.TotalRequired++
		if gd.Decision == StatusApproved {
			r.ApprovedCount++
		}
		if gd.LeaderID != nil && *gd.LeaderID == userID {
			r.MyLeaderGroups = append(r.MyLeaderGroups, gd.GroupName)
			if r.Status == StatusPendingReview && gd.Decision == StatusPendingReview {
				r.CanDecide = true
			}
		}
		r.Approvals = append(r.Approvals, gd)
	}
	return rows.Err()
}

// Approve mencatat persetujuan dari Leader Group. Status request baru berubah
// menjadi 'approved' apabila SEMUA group yang memiliki akses ke database tersebut
// telah menyetujui.
func (s *Service) Approve(ctx context.Context, userID, requestID uuid.UUID, note string) (*Request, error) {
	note = strings.TrimSpace(note)

	var dbID uuid.UUID
	var currentStatus string
	err := s.db.QueryRow(ctx, `SELECT database_id, status FROM approval_requests WHERE id = $1`, requestID).Scan(&dbID, &currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("approval: ambil request: %w", err)
	}

	canView, err := s.perms.CanView(ctx, userID, dbID)
	if err != nil {
		return nil, fmt.Errorf("approval: cek akses: %w", err)
	}
	if !canView {
		return nil, ErrNotFound
	}

	leadGroupIDs, err := s.perms.LeaderGroupsForDatabase(ctx, userID, dbID)
	if err != nil {
		return nil, fmt.Errorf("approval: cek leader groups: %w", err)
	}
	if len(leadGroupIDs) == 0 {
		return nil, ErrNotLeader
	}
	if Status(currentStatus) != StatusPendingReview {
		return nil, ErrAlreadyResolved
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("approval: begin approve: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx, `SELECT status FROM approval_requests WHERE id = $1 FOR UPDATE`, requestID).Scan(&currentStatus); err != nil {
		return nil, fmt.Errorf("approval: lock request: %w", err)
	}
	if Status(currentStatus) != StatusPendingReview {
		return nil, ErrAlreadyResolved
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO approval_group_decisions (request_id, group_id, decision)
		SELECT $1, gda.group_id, 'pending_review'
		FROM group_database_access gda
		WHERE gda.database_id = $2
		ON CONFLICT (request_id, group_id) DO NOTHING`,
		requestID, dbID,
	); err != nil {
		return nil, fmt.Errorf("approval: sync group decisions: %w", err)
	}

	tag, err := tx.Exec(ctx, `
		UPDATE approval_group_decisions
		SET decision = 'approved', decided_by = $3, note = $4, decided_at = now()
		WHERE request_id = $1 AND group_id = ANY($2) AND decision = 'pending_review'`,
		requestID, leadGroupIDs, userID, note,
	)
	if err != nil {
		return nil, fmt.Errorf("approval: update group decision: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrAlreadyDecided
	}

	var totalGroups, approvedGroups int
	if err := tx.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE COALESCE(agd.decision, 'pending_review') = 'approved')
		FROM group_database_access gda
		LEFT JOIN approval_group_decisions agd
		       ON agd.request_id = $1 AND agd.group_id = gda.group_id
		WHERE gda.database_id = $2`,
		requestID, dbID,
	).Scan(&totalGroups, &approvedGroups); err != nil {
		return nil, fmt.Errorf("approval: hitung quorum: %w", err)
	}

	allApproved := totalGroups > 0 && approvedGroups == totalGroups
	if allApproved {
		if _, err := tx.Exec(ctx, `
			UPDATE approval_requests
			SET status = 'approved', updated_at = now(), resolved_at = now()
			WHERE id = $1`, requestID,
		); err != nil {
			return nil, fmt.Errorf("approval: update status approved: %w", err)
		}
	} else {
		if _, err := tx.Exec(ctx, `
			UPDATE approval_requests
			SET updated_at = now()
			WHERE id = $1`, requestID,
		); err != nil {
			return nil, fmt.Errorf("approval: update timestamp: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("approval: commit approve: %w", err)
	}

	req, err := s.Get(ctx, userID, requestID)
	if err != nil {
		return nil, err
	}
	if allApproved && s.onApproved != nil {
		if err := s.onApproved(ctx, req); err != nil {
			return nil, err
		}
	}
	return req, nil
}

// Reject menolak permintaan approval. Satu penolakan dari salah satu Leader Group
// yang memiliki akses ke database langsung mengubah status request menjadi 'rejected'.
func (s *Service) Reject(ctx context.Context, userID, requestID uuid.UUID, reason string) (*Request, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, ErrEmptyReason
	}

	var dbID uuid.UUID
	var currentStatus string
	err := s.db.QueryRow(ctx, `SELECT database_id, status FROM approval_requests WHERE id = $1`, requestID).Scan(&dbID, &currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("approval: ambil request: %w", err)
	}

	canView, err := s.perms.CanView(ctx, userID, dbID)
	if err != nil {
		return nil, fmt.Errorf("approval: cek akses: %w", err)
	}
	if !canView {
		return nil, ErrNotFound
	}

	leadGroupIDs, err := s.perms.LeaderGroupsForDatabase(ctx, userID, dbID)
	if err != nil {
		return nil, fmt.Errorf("approval: cek leader groups: %w", err)
	}
	if len(leadGroupIDs) == 0 {
		return nil, ErrNotLeader
	}
	if Status(currentStatus) != StatusPendingReview {
		return nil, ErrAlreadyResolved
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("approval: begin reject: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := tx.QueryRow(ctx, `SELECT status FROM approval_requests WHERE id = $1 FOR UPDATE`, requestID).Scan(&currentStatus); err != nil {
		return nil, fmt.Errorf("approval: lock request: %w", err)
	}
	if Status(currentStatus) != StatusPendingReview {
		return nil, ErrAlreadyResolved
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO approval_group_decisions (request_id, group_id, decision)
		SELECT $1, gda.group_id, 'pending_review'
		FROM group_database_access gda
		WHERE gda.database_id = $2
		ON CONFLICT (request_id, group_id) DO NOTHING`,
		requestID, dbID,
	); err != nil {
		return nil, fmt.Errorf("approval: sync group decisions: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE approval_group_decisions
		SET decision = 'rejected', decided_by = $3, note = $4, decided_at = now()
		WHERE request_id = $1 AND group_id = ANY($2)`,
		requestID, leadGroupIDs, userID, reason,
	); err != nil {
		return nil, fmt.Errorf("approval: update group decision reject: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE approval_requests
		SET status = 'rejected', rejection_reason = $2, updated_at = now(), resolved_at = now()
		WHERE id = $1`,
		requestID, reason,
	); err != nil {
		return nil, fmt.Errorf("approval: update status rejected: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("approval: commit reject: %w", err)
	}

	return s.Get(ctx, userID, requestID)
}

// IsMigrationApproved mengecek apakah suatu migration sudah memiliki approval
// dengan status 'approved' untuk action tertentu.
func (s *Service) IsMigrationApproved(ctx context.Context, migrationID uuid.UUID, action Action) (bool, error) {
	if action == "" {
		action = ActionApply
	}
	var ok bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM approval_requests
			WHERE migration_id = $1 AND action = $2 AND status = 'approved'
		)`, migrationID, string(action),
	).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("approval: cek migration approved: %w", err)
	}
	return ok, nil
}
