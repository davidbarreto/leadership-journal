package store

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"leadership-journal/internal/domain"
)

const tagSeparator = "\x1f"

const entryColumns = `e.id, e.person_id, e.meeting_id, e.kind, e.body, e.outcome, e.occurred_at, e.occurred_tz,
	e.corrects_entry_id,
	(SELECT group_concat(t.name, char(31)) FROM entry_tag et JOIN tag t ON t.id = et.tag_id WHERE et.entry_id = e.id),
	e.created_at, e.created_tz, e.finalized_at, e.finalized_tz, e.voided_at, e.voided_tz, e.void_reason`

func scanEntry(r scanner) (domain.Entry, error) {
	var e domain.Entry
	var meeting, corrects sql.NullInt64
	var outcome, tags sql.NullString
	var occurredAt, occurredTZ string
	var lc lifecycleScan
	dests := append([]any{&e.ID, &e.PersonID, &meeting, &e.Kind, &e.Body, &outcome, &occurredAt, &occurredTZ, &corrects, &tags}, lc.dests()...)
	if err := r.Scan(dests...); err != nil {
		return e, err
	}
	e.MeetingID, e.CorrectsID = scanNullInt(meeting), scanNullInt(corrects)
	e.Outcome = domain.Outcome(outcome.String)
	if tags.Valid {
		e.Tags = strings.Split(tags.String, tagSeparator)
		sort.Strings(e.Tags)
	}
	var err error
	if e.OccurredAt, err = parseStamp(occurredAt, occurredTZ); err != nil {
		return e, err
	}
	e.Lifecycle, err = lc.build()
	return e, err
}

// CreateEntry validates an entry against its kind and stores it as a draft.
func (s *Store) CreateEntry(ctx context.Context, e domain.Entry) (int64, error) {
	var id int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		kind, err := getEntryKind(ctx, tx, e.Kind)
		if err != nil {
			return err
		}
		if err := e.Validate(kind); err != nil {
			return err
		}
		at, tz := formatStamp(s.now())
		occurredAt, occurredTZ := formatStamp(e.OccurredAt)
		res, err := tx.ExecContext(ctx,
			`INSERT INTO entry (person_id, meeting_id, kind, body, outcome, occurred_at, occurred_tz, corrects_entry_id, created_at, created_tz)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.PersonID, nullInt(e.MeetingID), e.Kind, e.Body, nullText(string(e.Outcome)), occurredAt, occurredTZ,
			nullInt(e.CorrectsID), at, tz)
		if err != nil {
			return wrapErr(err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		return setEntryTags(ctx, tx, id, e.Tags)
	})
	return id, err
}

func setEntryTags(ctx context.Context, q querier, entryID int64, names []string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM entry_tag WHERE entry_id = ?`, entryID); err != nil {
		return wrapErr(err)
	}
	ids, err := ensureTags(ctx, q, names)
	if err != nil {
		return err
	}
	for _, tagID := range ids {
		if _, err := q.ExecContext(ctx, `INSERT INTO entry_tag (entry_id, tag_id) VALUES (?, ?)`, entryID, tagID); err != nil {
			return wrapErr(err)
		}
	}
	return nil
}

// UpdateEntry rewrites a draft entry's kind, text, outcome, date and tags.
func (s *Store) UpdateEntry(ctx context.Context, e domain.Entry) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := requireDraft(ctx, tx, tableEntry, e.ID); err != nil {
			return err
		}
		kind, err := getEntryKind(ctx, tx, e.Kind)
		if err != nil {
			return err
		}
		if err := e.Validate(kind); err != nil {
			return err
		}
		occurredAt, occurredTZ := formatStamp(e.OccurredAt)
		if _, err := tx.ExecContext(ctx,
			`UPDATE entry SET kind = ?, body = ?, outcome = ?, occurred_at = ?, occurred_tz = ? WHERE id = ?`,
			e.Kind, e.Body, nullText(string(e.Outcome)), occurredAt, occurredTZ, e.ID); err != nil {
			return wrapErr(err)
		}
		return setEntryTags(ctx, tx, e.ID, e.Tags)
	})
}

// GetEntry returns one entry, or ErrNotFound.
func (s *Store) GetEntry(ctx context.Context, id int64) (domain.Entry, error) {
	e, err := scanEntry(s.db.QueryRowContext(ctx, `SELECT `+entryColumns+` FROM entry e WHERE e.id = ?`, id))
	return e, wrapErr(err)
}

// EntryFilter narrows ListEntries. Voided entries are hidden unless asked for.
type EntryFilter struct {
	PersonID      *int64
	MeetingID     *int64
	Kind          string
	Tag           string
	FinalizedOnly bool
	IncludeVoided bool
	Limit         int
}

// ListEntries returns entries by occurrence date, newest first.
func (s *Store) ListEntries(ctx context.Context, f EntryFilter) ([]domain.Entry, error) {
	var conds []string
	var args []any
	if f.PersonID != nil {
		conds, args = append(conds, "e.person_id = ?"), append(args, *f.PersonID)
	}
	if f.MeetingID != nil {
		conds, args = append(conds, "e.meeting_id = ?"), append(args, *f.MeetingID)
	}
	if f.Kind != "" {
		conds, args = append(conds, "e.kind = ?"), append(args, f.Kind)
	}
	if f.Tag != "" {
		conds = append(conds, `EXISTS (SELECT 1 FROM entry_tag et JOIN tag t ON t.id = et.tag_id WHERE et.entry_id = e.id AND t.name = ?)`)
		args = append(args, strings.ToLower(f.Tag))
	}
	if f.FinalizedOnly {
		conds = append(conds, "e.finalized_at IS NOT NULL")
	}
	if !f.IncludeVoided {
		conds = append(conds, "e.voided_at IS NULL")
	}
	return queryAll(ctx, s.db, func(r *sql.Rows) (domain.Entry, error) { return scanEntry(r) },
		`SELECT `+entryColumns+` FROM entry e`+where(conds)+` ORDER BY e.occurred_at DESC, e.id DESC`+limitClause(f.Limit), args...)
}

// FinalizeEntry locks a standalone draft entry (see ErrFinalizeWithMeeting).
func (s *Store) FinalizeEntry(ctx context.Context, id int64) error {
	return s.finalizeStandalone(ctx, tableEntry, id)
}

// VoidEntry declares a finalized entry wrong, keeping it for the record.
func (s *Store) VoidEntry(ctx context.Context, id int64, reason string) error {
	return s.voidStandalone(ctx, tableEntry, id, reason)
}

// DeleteDraftEntry removes an entry that was never finalized.
func (s *Store) DeleteDraftEntry(ctx context.Context, id int64) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := requireDraft(ctx, tx, tableEntry, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM entry_tag WHERE entry_id = ?`, id); err != nil {
			return wrapErr(err)
		}
		return deleteDraftRow(ctx, tx, tableEntry, id)
	})
}

// progressChecks selects the finalized, non-voided entries of kinds that carry
// an outcome: those are the improvement-journal checks. The kind decides this
// through its requires_outcome flag, never through a hard-coded code.
const progressChecks = `FROM entry e JOIN entry_kind k ON k.code = e.kind
	WHERE e.person_id = ? AND k.requires_outcome = 1 AND e.finalized_at IS NOT NULL AND e.voided_at IS NULL
	ORDER BY e.occurred_at DESC, e.id DESC`

// LastProgressCheck returns when the person's latest finalized progress check
// happened, or nil if there is none.
func (s *Store) LastProgressCheck(ctx context.Context, personID int64) (*time.Time, error) {
	var at, tz string
	err := s.db.QueryRowContext(ctx, `SELECT e.occurred_at, e.occurred_tz `+progressChecks+` LIMIT 1`, personID).Scan(&at, &tz)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, err := parseStamp(at, tz)
	return &t, err
}

// ProgressOutcomes returns the outcomes of the person's finalized progress
// checks, newest first.
func (s *Store) ProgressOutcomes(ctx context.Context, personID int64) ([]domain.Outcome, error) {
	return queryAll(ctx, s.db, func(r *sql.Rows) (domain.Outcome, error) {
		var o string
		err := r.Scan(&o)
		return domain.Outcome(o), err
	}, `SELECT e.outcome `+progressChecks, personID)
}
