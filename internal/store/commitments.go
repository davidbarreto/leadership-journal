package store

import (
	"context"
	"database/sql"
	"strings"

	"leadership-journal/internal/domain"
)

const commitmentColumns = `id, person_id, meeting_id, kind, direction, content, deliverable, due_on, status, status_note, ` + lifecycleColumns

func scanCommitment(r scanner) (domain.Commitment, error) {
	var c domain.Commitment
	var meeting sql.NullInt64
	var due sql.NullString
	var lc lifecycleScan
	dests := append([]any{&c.ID, &c.PersonID, &meeting, &c.Kind, &c.Direction, &c.Content, &c.Deliverable, &due, &c.Status, &c.StatusNote}, lc.dests()...)
	if err := r.Scan(dests...); err != nil {
		return c, err
	}
	c.MeetingID = scanNullInt(meeting)
	var err error
	if c.DueOn, err = scanNullDate(due); err != nil {
		return c, err
	}
	c.Lifecycle, err = lc.build()
	return c, err
}

// CreateCommitment stores a draft commitment. Its status log starts with an
// open event; the Status field of the argument is ignored.
func (s *Store) CreateCommitment(ctx context.Context, c domain.Commitment) (int64, error) {
	c.Status = domain.CommitmentOpen
	if err := c.Validate(); err != nil {
		return 0, err
	}
	var id int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		at, tz := formatStamp(s.now())
		res, err := tx.ExecContext(ctx,
			`INSERT INTO commitment (person_id, meeting_id, kind, direction, content, deliverable, due_on, created_at, created_tz)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.PersonID, nullInt(c.MeetingID), c.Kind, c.Direction, c.Content, c.Deliverable, nullDate(c.DueOn), at, tz)
		if err != nil {
			return wrapErr(err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		return addCommitmentEvent(ctx, tx, id, domain.CommitmentOpen, "", at, tz)
	})
	return id, err
}

func addCommitmentEvent(ctx context.Context, q querier, id int64, status domain.CommitmentStatus, note, at, tz string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO commitment_event (commitment_id, status, note, at, tz) VALUES (?, ?, ?, ?, ?)`,
		id, status, strings.TrimSpace(note), at, tz)
	return wrapErr(err)
}

// SetCommitmentStatus appends a status event. It works on locked commitments
// too, because the log is append-only history, not an edit of the commitment.
func (s *Store) SetCommitmentStatus(ctx context.Context, id int64, status domain.CommitmentStatus, note string) error {
	if !status.Valid() {
		return domain.ErrInvalidCommitmentStatus
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM commitment WHERE id = ? AND voided_at IS NULL`, id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		at, tz := formatStamp(s.now())
		return addCommitmentEvent(ctx, tx, id, status, note, at, tz)
	})
}

// GetCommitment returns one commitment with its current status, or ErrNotFound.
func (s *Store) GetCommitment(ctx context.Context, id int64) (domain.Commitment, error) {
	c, err := scanCommitment(s.db.QueryRowContext(ctx, `SELECT `+commitmentColumns+` FROM commitment_current WHERE id = ?`, id))
	return c, wrapErr(err)
}

// CommitmentFilter narrows ListCommitments. Voided ones are hidden unless asked for.
type CommitmentFilter struct {
	PersonID      *int64
	MeetingID     *int64
	OnlyOpen      bool
	FinalizedOnly bool
	IncludeVoided bool
}

// ListCommitments returns commitments ordered by due date (undated last).
func (s *Store) ListCommitments(ctx context.Context, f CommitmentFilter) ([]domain.Commitment, error) {
	var conds []string
	var args []any
	if f.PersonID != nil {
		conds, args = append(conds, "person_id = ?"), append(args, *f.PersonID)
	}
	if f.MeetingID != nil {
		conds, args = append(conds, "meeting_id = ?"), append(args, *f.MeetingID)
	}
	if f.OnlyOpen {
		conds = append(conds, "status = 'open'")
	}
	if f.FinalizedOnly {
		conds = append(conds, "finalized_at IS NOT NULL")
	}
	if !f.IncludeVoided {
		conds = append(conds, "voided_at IS NULL")
	}
	return queryAll(ctx, s.db, func(r *sql.Rows) (domain.Commitment, error) { return scanCommitment(r) },
		`SELECT `+commitmentColumns+` FROM commitment_current`+where(conds)+` ORDER BY due_on IS NULL, due_on, id`, args...)
}

// FinalizeCommitment locks a standalone draft commitment (see ErrFinalizeWithMeeting).
func (s *Store) FinalizeCommitment(ctx context.Context, id int64) error {
	return s.finalizeStandalone(ctx, tableCommitment, id)
}

// VoidCommitment declares a finalized commitment wrong, keeping it for the record.
func (s *Store) VoidCommitment(ctx context.Context, id int64, reason string) error {
	return s.voidStandalone(ctx, tableCommitment, id, reason)
}

// DeleteDraftCommitment removes a commitment that was never finalized.
func (s *Store) DeleteDraftCommitment(ctx context.Context, id int64) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := requireDraft(ctx, tx, tableCommitment, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM commitment_event WHERE commitment_id = ?`, id); err != nil {
			return wrapErr(err)
		}
		return deleteDraftRow(ctx, tx, tableCommitment, id)
	})
}
