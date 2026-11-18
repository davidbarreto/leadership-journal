package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"

	"leadership-journal/internal/domain"
)

// ReportStatus is what needs watching about one report: the 1:1 cadence and
// any run of progress checks without progress.
type ReportStatus struct {
	Person       domain.Person
	LastOneOnOne *time.Time
	// Overdue means the last 1:1 is more than IntervalDays ago, or there is none.
	Overdue          bool
	IntervalDays     int
	NoProgressStreak int
}

// ReportStatusOf computes the status of one report as of now.
func (s *Store) ReportStatusOf(ctx context.Context, p domain.Person, now time.Time, intervalDays int) (ReportStatus, error) {
	last, err := s.LastOneOnOne(ctx, p.ID)
	if err != nil {
		return ReportStatus{}, err
	}
	outcomes, err := s.ProgressOutcomes(ctx, p.ID)
	if err != nil {
		return ReportStatus{}, err
	}
	return ReportStatus{
		Person:           p,
		LastOneOnOne:     last,
		Overdue:          domain.OneOnOneOverdue(last, now, intervalDays),
		IntervalDays:     intervalDays,
		NoProgressStreak: domain.ConsecutiveNoProgress(outcomes),
	}, nil
}

// ReportStatuses returns the status of every report, in ListPeople order.
func (s *Store) ReportStatuses(ctx context.Context, now time.Time, intervalDays int) ([]ReportStatus, error) {
	people, err := s.ListPeople(ctx, domain.RelationshipReport)
	if err != nil {
		return nil, err
	}
	out := make([]ReportStatus, 0, len(people))
	for _, p := range people {
		st, err := s.ReportStatusOf(ctx, p, now, intervalDays)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// ComplianceRow is one report's standing for a release.
type ComplianceRow struct {
	Person    domain.Person
	Items     []domain.WorkItem
	Compliant bool
}

// ReleaseCompliance applies the "one stretch task per release" rule to every
// report, including those with no work items yet.
func (s *Store) ReleaseCompliance(ctx context.Context, releaseID int64) ([]ComplianceRow, error) {
	if _, err := s.GetRelease(ctx, releaseID); err != nil {
		return nil, err
	}
	people, err := s.ListPeople(ctx, domain.RelationshipReport)
	if err != nil {
		return nil, err
	}
	items, err := s.ListWorkItems(ctx, WorkFilter{ReleaseID: &releaseID})
	if err != nil {
		return nil, err
	}
	byPerson := map[int64][]domain.WorkItem{}
	for _, w := range items {
		byPerson[w.PersonID] = append(byPerson[w.PersonID], w)
	}
	out := make([]ComplianceRow, 0, len(people))
	for _, p := range people {
		out = append(out, ComplianceRow{Person: p, Items: byPerson[p.ID], Compliant: domain.ReleaseCompliant(byPerson[p.ID])})
	}
	return out, nil
}

// SearchHit is one full-text match with enough context to show and link it.
type SearchHit struct {
	Kind       string // entry, meeting or answer
	ID         int64
	MeetingID  *int64
	PersonID   *int64
	PersonName string
	Title      string
	// Snippet marks matches with \x1e (start) and \x1f (end). Callers must
	// HTML-escape the text first and only then turn the markers into tags.
	Snippet string
	At      time.Time
	Draft   bool
}

// ftsQuery turns free text into a safe FTS5 query: every word becomes a quoted
// prefix term, so operators and punctuation typed by the user cannot break the
// query syntax.
func ftsQuery(q string) string {
	words := strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	terms := make([]string, len(words))
	for i, w := range words {
		terms[i] = `"` + w + `"*`
	}
	return strings.Join(terms, " ")
}

const searchSQL = `
WITH hits AS (
	SELECT kind, ref_id, snippet(search_index, 2, char(30), char(31), '…', 12) AS snip, rank
	FROM search_index WHERE search_index MATCH ?1 ORDER BY rank LIMIT ?2
)
SELECT h.kind, e.id, e.meeting_id, e.person_id, p.name, k.label, h.snip, e.occurred_at, e.occurred_tz,
       e.finalized_at IS NULL, h.rank
FROM hits h JOIN entry e ON h.kind = 'entry' AND e.id = h.ref_id
JOIN person p ON p.id = e.person_id JOIN entry_kind k ON k.code = e.kind
WHERE e.voided_at IS NULL
UNION ALL
SELECT h.kind, m.id, m.id, m.person_id, COALESCE(p.name, ''), COALESCE(NULLIF(m.title, ''), mk.label), h.snip,
       m.starts_at, m.starts_tz, m.finalized_at IS NULL, h.rank
FROM hits h JOIN meeting m ON h.kind = 'meeting' AND m.id = h.ref_id
JOIN meeting_kind mk ON mk.code = m.kind LEFT JOIN person p ON p.id = m.person_id
WHERE m.voided_at IS NULL
UNION ALL
SELECT h.kind, a.id, a.meeting_id, m.person_id, COALESCE(p.name, ''), a.prompt_text, h.snip,
       m.starts_at, m.starts_tz, m.finalized_at IS NULL, h.rank
FROM hits h JOIN meeting_answer a ON h.kind = 'answer' AND a.id = h.ref_id
JOIN meeting m ON m.id = a.meeting_id LEFT JOIN person p ON p.id = m.person_id
WHERE m.voided_at IS NULL
ORDER BY 11`

// Search finds entries, meeting notes and meeting answers matching free text,
// best matches first. Voided records never appear.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	return queryAll(ctx, s.db, func(r *sql.Rows) (SearchHit, error) {
		var h SearchHit
		var meeting, person sql.NullInt64
		var at, tz string
		var rank float64
		if err := r.Scan(&h.Kind, &h.ID, &meeting, &person, &h.PersonName, &h.Title, &h.Snippet, &at, &tz, &h.Draft, &rank); err != nil {
			return h, err
		}
		h.MeetingID, h.PersonID = scanNullInt(meeting), scanNullInt(person)
		var err error
		h.At, err = parseStamp(at, tz)
		return h, err
	}, searchSQL, match, limit)
}
