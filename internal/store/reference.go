package store

import (
	"context"
	"database/sql"
	"strings"

	"leadership-journal/internal/domain"
)

// CreateMeetingKind adds a meeting kind; its code is derived from the label.
func (s *Store) CreateMeetingKind(ctx context.Context, label string, requiresPerson bool) (domain.MeetingKind, error) {
	label = strings.TrimSpace(label)
	code := domain.CodeFromLabel(label)
	if code == "" {
		return domain.MeetingKind{}, domain.ErrCodeRequired
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO meeting_kind (code, label, sort_order, requires_person)
		 VALUES (?, ?, (SELECT COALESCE(MAX(sort_order), 0) + 10 FROM meeting_kind), ?)`,
		code, label, requiresPerson)
	if err != nil {
		return domain.MeetingKind{}, wrapErr(err)
	}
	return domain.MeetingKind{Code: code, Label: label, RequiresPerson: requiresPerson}, nil
}

const meetingKindColumns = `code, label, requires_person, archived`

func scanMeetingKind(r interface{ Scan(...any) error }) (domain.MeetingKind, error) {
	var k domain.MeetingKind
	err := r.Scan(&k.Code, &k.Label, &k.RequiresPerson, &k.Archived)
	return k, err
}

// ListMeetingKinds returns the active meeting kinds in display order.
func (s *Store) ListMeetingKinds(ctx context.Context) ([]domain.MeetingKind, error) {
	return queryAll(ctx, s.db, func(r *sql.Rows) (domain.MeetingKind, error) { return scanMeetingKind(r) },
		`SELECT `+meetingKindColumns+` FROM meeting_kind WHERE archived = 0 ORDER BY sort_order, code`)
}

// GetMeetingKind returns one kind (archived kinds included, since old
// meetings still refer to them), or ErrNotFound.
func (s *Store) GetMeetingKind(ctx context.Context, code string) (domain.MeetingKind, error) {
	return getMeetingKind(ctx, s.db, code)
}

func getMeetingKind(ctx context.Context, q querier, code string) (domain.MeetingKind, error) {
	k, err := scanMeetingKind(q.QueryRowContext(ctx, `SELECT `+meetingKindColumns+` FROM meeting_kind WHERE code = ?`, code))
	return k, wrapErr(err)
}

// CreateEntryKind adds an entry kind; its code is derived from the label.
func (s *Store) CreateEntryKind(ctx context.Context, label string, requiresOutcome bool) (domain.EntryKind, error) {
	label = strings.TrimSpace(label)
	code := domain.CodeFromLabel(label)
	if code == "" {
		return domain.EntryKind{}, domain.ErrCodeRequired
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO entry_kind (code, label, sort_order, requires_outcome)
		 VALUES (?, ?, (SELECT COALESCE(MAX(sort_order), 0) + 10 FROM entry_kind), ?)`,
		code, label, requiresOutcome)
	if err != nil {
		return domain.EntryKind{}, wrapErr(err)
	}
	return domain.EntryKind{Code: code, Label: label, RequiresOutcome: requiresOutcome}, nil
}

const entryKindColumns = `code, label, requires_outcome, archived`

func scanEntryKind(r interface{ Scan(...any) error }) (domain.EntryKind, error) {
	var k domain.EntryKind
	err := r.Scan(&k.Code, &k.Label, &k.RequiresOutcome, &k.Archived)
	return k, err
}

// ListEntryKinds returns the active entry kinds in display order.
func (s *Store) ListEntryKinds(ctx context.Context) ([]domain.EntryKind, error) {
	return queryAll(ctx, s.db, func(r *sql.Rows) (domain.EntryKind, error) { return scanEntryKind(r) },
		`SELECT `+entryKindColumns+` FROM entry_kind WHERE archived = 0 ORDER BY sort_order, code`)
}

// GetEntryKind returns one kind (archived included), or ErrNotFound.
func (s *Store) GetEntryKind(ctx context.Context, code string) (domain.EntryKind, error) {
	return getEntryKind(ctx, s.db, code)
}

func getEntryKind(ctx context.Context, q querier, code string) (domain.EntryKind, error) {
	k, err := scanEntryKind(q.QueryRowContext(ctx, `SELECT `+entryKindColumns+` FROM entry_kind WHERE code = ?`, code))
	return k, wrapErr(err)
}

const maxPromptKeyLength = 40

// CreatePrompt adds a question to a meeting kind's minutes template. The key
// is derived from the text and is what answers are stored under.
func (s *Store) CreatePrompt(ctx context.Context, meetingKind, text string) (domain.Prompt, error) {
	text = strings.TrimSpace(text)
	key := domain.CodeFromLabel(text)
	if len(key) > maxPromptKeyLength {
		key = strings.TrimRight(key[:maxPromptKeyLength], "_")
	}
	if key == "" {
		return domain.Prompt{}, domain.ErrContentRequired
	}
	if _, err := s.GetMeetingKind(ctx, meetingKind); err != nil {
		return domain.Prompt{}, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO meeting_prompt (meeting_kind, prompt_key, prompt_text, sort_order)
		 VALUES (?, ?, ?, (SELECT COALESCE(MAX(sort_order), 0) + 10 FROM meeting_prompt WHERE meeting_kind = ?))`,
		meetingKind, key, text, meetingKind)
	if err != nil {
		return domain.Prompt{}, wrapErr(err)
	}
	id, err := res.LastInsertId()
	return domain.Prompt{ID: id, MeetingKind: meetingKind, Key: key, Text: text}, err
}

// ListPrompts returns the active questions of a meeting kind in order.
func (s *Store) ListPrompts(ctx context.Context, meetingKind string) ([]domain.Prompt, error) {
	return queryAll(ctx, s.db, func(r *sql.Rows) (domain.Prompt, error) {
		var p domain.Prompt
		err := r.Scan(&p.ID, &p.MeetingKind, &p.Key, &p.Text)
		return p, err
	}, `SELECT id, meeting_kind, prompt_key, prompt_text FROM meeting_prompt
	    WHERE meeting_kind = ? AND archived = 0 ORDER BY sort_order, id`, meetingKind)
}

// ListTags returns the active tag names sorted alphabetically.
func (s *Store) ListTags(ctx context.Context) ([]string, error) {
	return queryAll(ctx, s.db, func(r *sql.Rows) (string, error) {
		var name string
		err := r.Scan(&name)
		return name, err
	}, `SELECT name FROM tag WHERE archived = 0 ORDER BY name`)
}

// ensureTags makes sure every tag name exists and returns the ids. Names are
// normalized to lowercase so "Ownership" and "ownership" are one tag.
func ensureTags(ctx context.Context, q querier, names []string) ([]int64, error) {
	seen := map[string]bool{}
	var ids []int64
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO tag (name) VALUES (?)`, name); err != nil {
			return nil, err
		}
		var id int64
		if err := q.QueryRowContext(ctx, `SELECT id FROM tag WHERE name = ?`, name).Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// CreateRelease adds a release.
func (s *Store) CreateRelease(ctx context.Context, r domain.Release) (int64, error) {
	if err := r.Validate(); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO release (name, starts_on, ends_on) VALUES (?, ?, ?)`,
		strings.TrimSpace(r.Name), formatDate(r.StartsOn), formatDate(r.EndsOn))
	if err != nil {
		return 0, wrapErr(err)
	}
	return res.LastInsertId()
}

const releaseColumns = `id, name, starts_on, ends_on`

func scanRelease(r interface{ Scan(...any) error }) (domain.Release, error) {
	var rel domain.Release
	var starts, ends string
	if err := r.Scan(&rel.ID, &rel.Name, &starts, &ends); err != nil {
		return rel, err
	}
	var err error
	if rel.StartsOn, err = parseDate(starts); err != nil {
		return rel, err
	}
	rel.EndsOn, err = parseDate(ends)
	return rel, err
}

// ListReleases returns releases, newest first.
func (s *Store) ListReleases(ctx context.Context) ([]domain.Release, error) {
	return queryAll(ctx, s.db, func(r *sql.Rows) (domain.Release, error) { return scanRelease(r) },
		`SELECT `+releaseColumns+` FROM release ORDER BY starts_on DESC, id DESC`)
}

// GetRelease returns one release, or ErrNotFound.
func (s *Store) GetRelease(ctx context.Context, id int64) (domain.Release, error) {
	r, err := scanRelease(s.db.QueryRowContext(ctx, `SELECT `+releaseColumns+` FROM release WHERE id = ?`, id))
	return r, wrapErr(err)
}
