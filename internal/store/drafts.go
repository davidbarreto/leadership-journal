package store

import (
	"context"
	"database/sql"

	"leadership-journal/internal/domain"
)

// DraftCounts is how many records still wait to be finalized. Entries and
// commitments noted in a meeting are not counted apart: they finalize with
// the meeting.
type DraftCounts struct {
	Meetings, Entries, Commitments, WorkItems int
}

// Total is the number of drafts of every kind.
func (c DraftCounts) Total() int { return c.Meetings + c.Entries + c.Commitments + c.WorkItems }

// PendingDrafts counts the drafts waiting to be finalized, as ListDrafts
// lists them.
func (s *Store) PendingDrafts(ctx context.Context) (DraftCounts, error) {
	var c DraftCounts
	for _, item := range []struct {
		query string
		dst   *int
	}{
		{`SELECT COUNT(*) FROM meeting WHERE finalized_at IS NULL`, &c.Meetings},
		{`SELECT COUNT(*) FROM entry WHERE finalized_at IS NULL AND meeting_id IS NULL`, &c.Entries},
		{`SELECT COUNT(*) FROM commitment WHERE finalized_at IS NULL AND meeting_id IS NULL`, &c.Commitments},
		{`SELECT COUNT(*) FROM work_item WHERE finalized_at IS NULL`, &c.WorkItems},
	} {
		if err := s.db.QueryRowContext(ctx, item.query).Scan(item.dst); err != nil {
			return c, err
		}
	}
	return c, nil
}

// Drafts is every record waiting to be finalized. Entries and commitments
// noted in a meeting are not listed apart; InMeeting counts them per meeting
// id instead.
type Drafts struct {
	Meetings    []domain.Meeting
	InMeeting   map[int64]int
	Entries     []domain.Entry
	Commitments []domain.Commitment
	WorkItems   []domain.WorkItem
}

// ListDrafts returns the drafts, newest first within each kind.
func (s *Store) ListDrafts(ctx context.Context) (Drafts, error) {
	var d Drafts
	var err error
	if d.Meetings, err = queryAll(ctx, s.db, func(r *sql.Rows) (domain.Meeting, error) { return scanMeeting(r) },
		`SELECT `+meetingColumns+` FROM meeting WHERE finalized_at IS NULL ORDER BY starts_at DESC, id DESC`); err != nil {
		return d, err
	}
	type meetingCount struct{ id, n int64 }
	counts, err := queryAll(ctx, s.db, func(r *sql.Rows) (meetingCount, error) {
		var c meetingCount
		err := r.Scan(&c.id, &c.n)
		return c, err
	}, `SELECT meeting_id, COUNT(*) FROM (
			SELECT meeting_id FROM entry WHERE meeting_id IS NOT NULL AND finalized_at IS NULL
			UNION ALL
			SELECT meeting_id FROM commitment WHERE meeting_id IS NOT NULL AND finalized_at IS NULL
		) GROUP BY meeting_id`)
	if err != nil {
		return d, err
	}
	d.InMeeting = make(map[int64]int, len(counts))
	for _, c := range counts {
		d.InMeeting[c.id] = int(c.n)
	}
	if d.Entries, err = queryAll(ctx, s.db, func(r *sql.Rows) (domain.Entry, error) { return scanEntry(r) },
		`SELECT `+entryColumns+` FROM entry e WHERE e.finalized_at IS NULL AND e.meeting_id IS NULL
		 ORDER BY e.occurred_at DESC, e.id DESC`); err != nil {
		return d, err
	}
	if d.Commitments, err = queryAll(ctx, s.db, func(r *sql.Rows) (domain.Commitment, error) { return scanCommitment(r) },
		`SELECT `+commitmentColumns+` FROM commitment_current WHERE finalized_at IS NULL AND meeting_id IS NULL
		 ORDER BY created_at DESC, id DESC`); err != nil {
		return d, err
	}
	d.WorkItems, err = queryAll(ctx, s.db, func(r *sql.Rows) (domain.WorkItem, error) { return scanWorkItem(r) },
		`SELECT `+workColumns+` FROM work_item WHERE finalized_at IS NULL ORDER BY assigned_at DESC, id DESC`)
	return d, err
}

// DraftSelection names drafts to finalize together, by id per kind.
type DraftSelection struct {
	Meetings, Entries, Commitments, WorkItems []int64
}

// Len is the number of drafts selected.
func (sel DraftSelection) Len() int {
	return len(sel.Meetings) + len(sel.Entries) + len(sel.Commitments) + len(sel.WorkItems)
}

// FinalizeDrafts finalizes the selected drafts in one transaction, each the
// way it would be on its own (a meeting with everything noted in it). If any
// cannot be finalized, none is.
func (s *Store) FinalizeDrafts(ctx context.Context, sel DraftSelection) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		for _, id := range sel.Meetings {
			if err := s.finalizeMeeting(ctx, tx, id); err != nil {
				return err
			}
		}
		for _, id := range sel.Entries {
			if err := s.finalizeStandaloneIn(ctx, tx, tableEntry, id); err != nil {
				return err
			}
		}
		for _, id := range sel.Commitments {
			if err := s.finalizeStandaloneIn(ctx, tx, tableCommitment, id); err != nil {
				return err
			}
		}
		at, tz := formatStamp(s.now())
		for _, id := range sel.WorkItems {
			if err := finalizeRow(ctx, tx, tableWorkItem, id, at, tz); err != nil {
				return err
			}
		}
		return nil
	})
}
