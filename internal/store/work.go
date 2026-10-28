package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"leadership-journal/internal/domain"
)

const workColumns = `id, person_id, release_id, title, external_ref, picked_by, stretch, stretch_rationale, status,
	investigation_done, investigation_url, outcome_notes, assigned_at, assigned_tz, completed_at, completed_tz,
	moved_from_id, ` + lifecycleColumns

func scanWorkItem(r scanner) (domain.WorkItem, error) {
	var w domain.WorkItem
	var ref, completedAt, completedTZ sql.NullString
	var moved sql.NullInt64
	var assignedAt, assignedTZ string
	var lc lifecycleScan
	dests := append([]any{&w.ID, &w.PersonID, &w.ReleaseID, &w.Title, &ref, &w.PickedBy, &w.Stretch, &w.StretchRationale,
		&w.Status, &w.InvestigationDone, &w.InvestigationURL, &w.OutcomeNotes, &assignedAt, &assignedTZ,
		&completedAt, &completedTZ, &moved}, lc.dests()...)
	if err := r.Scan(dests...); err != nil {
		return w, err
	}
	w.ExternalRef = ref.String
	w.MovedFromID = scanNullInt(moved)
	var err error
	if w.AssignedAt, err = parseStamp(assignedAt, assignedTZ); err != nil {
		return w, err
	}
	if w.CompletedAt, err = scanNullStamp(completedAt, completedTZ); err != nil {
		return w, err
	}
	w.Lifecycle, err = lc.build()
	return w, err
}

// withDefaults fills what a new work item may leave out.
func (s *Store) withDefaults(w domain.WorkItem) domain.WorkItem {
	if w.Status == "" {
		w.Status = domain.WorkPlanned
	}
	if w.AssignedAt.IsZero() {
		w.AssignedAt = s.now()
	}
	w.ExternalRef = strings.TrimSpace(w.ExternalRef)
	return w
}

// CreateWorkItem validates and stores a draft work item.
func (s *Store) CreateWorkItem(ctx context.Context, w domain.WorkItem) (int64, error) {
	w = s.withDefaults(w)
	if err := w.Validate(); err != nil {
		return 0, err
	}
	return s.insertWorkItem(ctx, s.db, w)
}

func (s *Store) insertWorkItem(ctx context.Context, q querier, w domain.WorkItem) (int64, error) {
	at, tz := formatStamp(s.now())
	assignedAt, assignedTZ := formatStamp(w.AssignedAt)
	res, err := q.ExecContext(ctx,
		`INSERT INTO work_item (person_id, release_id, title, external_ref, picked_by, stretch, stretch_rationale, status,
			investigation_done, investigation_url, outcome_notes, assigned_at, assigned_tz, moved_from_id, created_at, created_tz)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.PersonID, w.ReleaseID, w.Title, nullText(w.ExternalRef), w.PickedBy, w.Stretch, w.StretchRationale, w.Status,
		w.InvestigationDone, w.InvestigationURL, w.OutcomeNotes, assignedAt, assignedTZ, nullInt(w.MovedFromID), at, tz)
	if err != nil {
		return 0, wrapErr(err)
	}
	return res.LastInsertId()
}

// UpdateWorkItem rewrites a draft work item, judgment included. Once
// finalized, only UpdateWorkTracking can change it.
func (s *Store) UpdateWorkItem(ctx context.Context, w domain.WorkItem) error {
	w = s.withDefaults(w)
	if w.Status == domain.WorkMoved {
		return domain.ErrInvalidWorkStatus // only MoveWorkItem moves a task
	}
	if err := w.Validate(); err != nil {
		return err
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := requireDraft(ctx, tx, tableWorkItem, w.ID); err != nil {
			return err
		}
		assignedAt, assignedTZ := formatStamp(w.AssignedAt)
		_, err := tx.ExecContext(ctx,
			`UPDATE work_item SET person_id = ?, release_id = ?, title = ?, external_ref = ?, picked_by = ?, stretch = ?,
				stretch_rationale = ?, status = ?, investigation_done = ?, investigation_url = ?, outcome_notes = ?,
				assigned_at = ?, assigned_tz = ? WHERE id = ?`,
			w.PersonID, w.ReleaseID, w.Title, nullText(w.ExternalRef), w.PickedBy, w.Stretch, w.StretchRationale, w.Status,
			w.InvestigationDone, w.InvestigationURL, w.OutcomeNotes, assignedAt, assignedTZ, w.ID)
		return wrapErr(err)
	})
}

// WorkTracking holds the fields of a work item that keep changing as the work
// progresses, and so stay editable after the item is finalized.
type WorkTracking struct {
	Status            domain.WorkStatus
	InvestigationDone bool
	InvestigationURL  string
	OutcomeNotes      string
	CompletedAt       *time.Time // defaults to now when Status is done
}

// UpdateWorkTracking changes a work item's progress fields in any lifecycle
// state. The completion date is set when the status is done and cleared when
// the item is reopened. Moving a task is MoveWorkItem's job.
func (s *Store) UpdateWorkTracking(ctx context.Context, id int64, t WorkTracking) error {
	switch t.Status {
	case domain.WorkPlanned, domain.WorkInProgress, domain.WorkDone, domain.WorkDropped:
	default:
		return domain.ErrInvalidWorkStatus
	}
	var completed *time.Time
	if t.Status == domain.WorkDone {
		c := s.now()
		if t.CompletedAt != nil {
			c = *t.CompletedAt
		}
		completed = &c
	}
	completedAt, completedTZ := nullStamp(completed)
	res, err := s.db.ExecContext(ctx,
		`UPDATE work_item SET status = ?, investigation_done = ?, investigation_url = ?, outcome_notes = ?,
			completed_at = ?, completed_tz = ?
		 WHERE id = ? AND voided_at IS NULL AND status <> 'moved'`,
		t.Status, t.InvestigationDone, strings.TrimSpace(t.InvestigationURL), t.OutcomeNotes, completedAt, completedTZ, id)
	if err != nil {
		return wrapErr(err)
	}
	return requireAffected(res)
}

// MoveWorkItem reassigns a task that is not finished. The original row is kept
// with status moved, and a new draft row is created for the new person, who
// gets their own stretch judgment (taken from `to`). It returns the new id.
func (s *Store) MoveWorkItem(ctx context.Context, id int64, to domain.WorkItem) (int64, error) {
	var newID int64
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		orig, err := scanWorkItem(tx.QueryRowContext(ctx,
			`SELECT `+workColumns+` FROM work_item WHERE id = ? AND voided_at IS NULL`, id))
		if err != nil {
			return wrapErr(err)
		}
		if orig.Status != domain.WorkPlanned && orig.Status != domain.WorkInProgress {
			return ErrCannotMove
		}
		if to.PersonID == orig.PersonID {
			return fmt.Errorf("%w: choose a different person", ErrCannotMove)
		}
		next := s.withDefaults(domain.WorkItem{
			PersonID:         to.PersonID,
			ReleaseID:        orig.ReleaseID,
			Title:            orig.Title,
			ExternalRef:      orig.ExternalRef,
			PickedBy:         to.PickedBy,
			Stretch:          to.Stretch,
			StretchRationale: to.StretchRationale,
			AssignedAt:       to.AssignedAt,
			MovedFromID:      &id,
		})
		// Validate before touching the original so a bad judgment changes nothing.
		if err := next.Validate(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE work_item SET status = 'moved' WHERE id = ?`, id); err != nil {
			return wrapErr(err)
		}
		newID, err = s.insertWorkItem(ctx, tx, next)
		return err
	})
	return newID, err
}

// GetWorkItem returns one work item, or ErrNotFound.
func (s *Store) GetWorkItem(ctx context.Context, id int64) (domain.WorkItem, error) {
	w, err := scanWorkItem(s.db.QueryRowContext(ctx, `SELECT `+workColumns+` FROM work_item WHERE id = ?`, id))
	return w, wrapErr(err)
}

// WorkFilter narrows ListWorkItems. Voided items are hidden unless asked for.
type WorkFilter struct {
	PersonID      *int64
	ReleaseID     *int64
	IncludeVoided bool
}

// ListWorkItems returns work items by assignment date, newest first.
func (s *Store) ListWorkItems(ctx context.Context, f WorkFilter) ([]domain.WorkItem, error) {
	var conds []string
	var args []any
	if f.PersonID != nil {
		conds, args = append(conds, "person_id = ?"), append(args, *f.PersonID)
	}
	if f.ReleaseID != nil {
		conds, args = append(conds, "release_id = ?"), append(args, *f.ReleaseID)
	}
	if !f.IncludeVoided {
		conds = append(conds, "voided_at IS NULL")
	}
	return queryAll(ctx, s.db, func(r *sql.Rows) (domain.WorkItem, error) { return scanWorkItem(r) },
		`SELECT `+workColumns+` FROM work_item`+where(conds)+` ORDER BY assigned_at DESC, id DESC`, args...)
}

// FinalizeWorkItem locks the stretch judgment of a draft work item.
func (s *Store) FinalizeWorkItem(ctx context.Context, id int64) error {
	at, tz := formatStamp(s.now())
	return finalizeRow(ctx, s.db, tableWorkItem, id, at, tz)
}

// VoidWorkItem declares a finalized work item wrong, keeping it for the record.
func (s *Store) VoidWorkItem(ctx context.Context, id int64, reason string) error {
	at, tz := formatStamp(s.now())
	return voidRow(ctx, s.db, tableWorkItem, id, reason, at, tz)
}

// DeleteDraftWorkItem removes a work item that was never finalized.
func (s *Store) DeleteDraftWorkItem(ctx context.Context, id int64) error {
	return deleteDraftRow(ctx, s.db, tableWorkItem, id)
}
