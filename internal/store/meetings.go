package store

import (
	"context"
	"database/sql"
	"strings"

	"leadership-journal/internal/domain"
)

const meetingColumns = `id, kind, title, person_id, team_id, starts_at, starts_tz, notes, ` + lifecycleColumns

func scanMeeting(r scanner) (domain.Meeting, error) {
	var m domain.Meeting
	var person, team sql.NullInt64
	var startsAt, startsTZ string
	var lc lifecycleScan
	dests := append([]any{&m.ID, &m.Kind, &m.Title, &person, &team, &startsAt, &startsTZ, &m.Notes}, lc.dests()...)
	if err := r.Scan(dests...); err != nil {
		return m, err
	}
	m.PersonID, m.TeamID = scanNullInt(person), scanNullInt(team)
	var err error
	if m.StartsAt, err = parseStamp(startsAt, startsTZ); err != nil {
		return m, err
	}
	m.Lifecycle, err = lc.build()
	return m, err
}

// CreateMeeting stores a draft meeting with its non-empty answers.
func (s *Store) CreateMeeting(ctx context.Context, m domain.Meeting) (int64, error) {
	var id int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		kind, err := getMeetingKind(ctx, tx, m.Kind)
		if err != nil {
			return err
		}
		if err := m.Validate(kind); err != nil {
			return err
		}
		at, tz := formatStamp(s.now())
		startsAt, startsTZ := formatStamp(m.StartsAt)
		res, err := tx.ExecContext(ctx,
			`INSERT INTO meeting (kind, title, person_id, team_id, starts_at, starts_tz, notes, created_at, created_tz)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.Kind, m.Title, nullInt(m.PersonID), nullInt(m.TeamID), startsAt, startsTZ, m.Notes, at, tz)
		if err != nil {
			return wrapErr(err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		return insertAnswers(ctx, tx, id, m.Answers)
	})
	return id, err
}

func insertAnswers(ctx context.Context, q querier, meetingID int64, answers []domain.Answer) error {
	for _, a := range answers {
		if strings.TrimSpace(a.Text) == "" {
			continue
		}
		if _, err := q.ExecContext(ctx,
			`INSERT INTO meeting_answer (meeting_id, prompt_key, prompt_text, answer) VALUES (?, ?, ?, ?)`,
			meetingID, a.PromptKey, a.PromptText, a.Text); err != nil {
			return wrapErr(err)
		}
	}
	return nil
}

// UpdateMeeting rewrites a draft meeting, replacing its answers.
func (s *Store) UpdateMeeting(ctx context.Context, m domain.Meeting) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := requireDraft(ctx, tx, tableMeeting, m.ID); err != nil {
			return err
		}
		kind, err := getMeetingKind(ctx, tx, m.Kind)
		if err != nil {
			return err
		}
		if err := m.Validate(kind); err != nil {
			return err
		}
		startsAt, startsTZ := formatStamp(m.StartsAt)
		if _, err := tx.ExecContext(ctx,
			`UPDATE meeting SET kind = ?, title = ?, person_id = ?, team_id = ?, starts_at = ?, starts_tz = ?, notes = ? WHERE id = ?`,
			m.Kind, m.Title, nullInt(m.PersonID), nullInt(m.TeamID), startsAt, startsTZ, m.Notes, m.ID); err != nil {
			return wrapErr(err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM meeting_answer WHERE meeting_id = ?`, m.ID); err != nil {
			return wrapErr(err)
		}
		return insertAnswers(ctx, tx, m.ID, m.Answers)
	})
}

// GetMeeting returns a meeting with its answers, or ErrNotFound.
func (s *Store) GetMeeting(ctx context.Context, id int64) (domain.Meeting, error) {
	m, err := scanMeeting(s.db.QueryRowContext(ctx, `SELECT `+meetingColumns+` FROM meeting WHERE id = ?`, id))
	if err != nil {
		return m, wrapErr(err)
	}
	m.Answers, err = queryAll(ctx, s.db, func(r *sql.Rows) (domain.Answer, error) {
		var a domain.Answer
		err := r.Scan(&a.PromptKey, &a.PromptText, &a.Text)
		return a, err
	}, `SELECT prompt_key, prompt_text, answer FROM meeting_answer WHERE meeting_id = ? ORDER BY id`, id)
	return m, err
}

// MeetingFilter narrows ListMeetings. Voided meetings are hidden unless asked for.
type MeetingFilter struct {
	PersonID      *int64
	IncludeVoided bool
	Limit         int
}

// ListMeetings returns meetings newest first, without their answers.
func (s *Store) ListMeetings(ctx context.Context, f MeetingFilter) ([]domain.Meeting, error) {
	var conds []string
	var args []any
	if f.PersonID != nil {
		conds, args = append(conds, "person_id = ?"), append(args, *f.PersonID)
	}
	if !f.IncludeVoided {
		conds = append(conds, "voided_at IS NULL")
	}
	return queryAll(ctx, s.db, func(r *sql.Rows) (domain.Meeting, error) { return scanMeeting(r) },
		`SELECT `+meetingColumns+` FROM meeting`+where(conds)+` ORDER BY starts_at DESC, id DESC`+limitClause(f.Limit), args...)
}

// FinalizeMeeting locks a draft meeting together with the answers, entries and
// commitments created in it.
func (s *Store) FinalizeMeeting(ctx context.Context, id int64) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		at, tz := formatStamp(s.now())
		if err := finalizeRow(ctx, tx, tableMeeting, id, at, tz); err != nil {
			return err
		}
		for _, t := range []table{tableEntry, tableCommitment} {
			if _, err := tx.ExecContext(ctx,
				`UPDATE `+string(t)+` SET finalized_at = ?, finalized_tz = ? WHERE meeting_id = ? AND finalized_at IS NULL`,
				at, tz, id); err != nil {
				return wrapErr(err)
			}
		}
		return nil
	})
}

// VoidMeeting declares a finalized meeting wrong. Its entries and commitments
// are voided with it, since they were recorded in that meeting.
func (s *Store) VoidMeeting(ctx context.Context, id int64, reason string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		at, tz := formatStamp(s.now())
		if err := voidRow(ctx, tx, tableMeeting, id, reason, at, tz); err != nil {
			return err
		}
		for _, t := range []table{tableEntry, tableCommitment} {
			if _, err := tx.ExecContext(ctx,
				`UPDATE `+string(t)+` SET voided_at = ?, voided_tz = ?, void_reason = ?
				 WHERE meeting_id = ? AND finalized_at IS NOT NULL AND voided_at IS NULL`,
				at, tz, strings.TrimSpace(reason), id); err != nil {
				return wrapErr(err)
			}
		}
		return nil
	})
}

// DeleteDraftMeeting removes a draft meeting and everything created in it.
func (s *Store) DeleteDraftMeeting(ctx context.Context, id int64) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := requireDraft(ctx, tx, tableMeeting, id); err != nil {
			return err
		}
		// Children first (foreign keys). A draft meeting can only hold draft
		// children: meeting-linked rows are never finalized on their own.
		for _, stmt := range []string{
			`DELETE FROM entry_tag WHERE entry_id IN (SELECT id FROM entry WHERE meeting_id = ?1)`,
			`DELETE FROM entry WHERE meeting_id = ?1`,
			`DELETE FROM commitment_event WHERE commitment_id IN (SELECT id FROM commitment WHERE meeting_id = ?1)`,
			`DELETE FROM commitment WHERE meeting_id = ?1`,
			`DELETE FROM meeting_answer WHERE meeting_id = ?1`,
			`DELETE FROM meeting WHERE id = ?1`,
		} {
			if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
				return wrapErr(err)
			}
		}
		return nil
	})
}
