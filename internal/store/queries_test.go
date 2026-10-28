package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"leadership-journal/internal/domain"
)

func TestFTSQuery(t *testing.T) {
	tests := []struct{ in, want string }{
		{"outage", `"outage"*`},
		{"  Kubernetes   migration ", `"Kubernetes"* "migration"*`},
		{`owner-ship AND (x)`, `"owner"* "ship"* "AND"* "x"*`},
		{`"quoted" OR NOT *`, `"quoted"* "OR"* "NOT"*`},
		{"café Zoë", `"café"* "Zoë"*`},
		{"", ""},
		{"!!! ---", ""},
	}
	for _, tc := range tests {
		if got := ftsQuery(tc.in); got != tc.want {
			t.Errorf("ftsQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSearch_FindsEntriesMeetingsAndAnswers(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)

	mustEntry(t, s, newEntry(p, nil, "Showed great ownership during the outage"))
	mid, _ := s.CreateMeeting(ctx, domain.Meeting{
		Kind: "one_on_one", Title: "Quarterly sync", PersonID: &p, StartsAt: day(2024, 3, 1),
		Notes:   "Discussed the outage follow-ups",
		Answers: []domain.Answer{{PromptKey: "learned", PromptText: "What did you learn?", Text: "Incident outage response"}},
	})

	hits, err := s.Search(ctx, "outage", 20)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, h := range hits {
		kinds[h.Kind] = true
		if h.PersonName != "Ana" || h.PersonID == nil || *h.PersonID != p {
			t.Errorf("hit lacks its person: %+v", h)
		}
		if !strings.Contains(h.Snippet, "\x1e") || !strings.Contains(h.Snippet, "\x1f") {
			t.Errorf("snippet %q should mark the match with control characters for the caller to escape and highlight", h.Snippet)
		}
		if !h.Draft {
			t.Errorf("hit should be marked as a draft: %+v", h)
		}
	}
	if len(hits) != 3 || !kinds["entry"] || !kinds["meeting"] || !kinds["answer"] {
		t.Fatalf("hits = %+v, want one entry, one meeting and one answer", hits)
	}
	for _, h := range hits {
		if h.Kind != "entry" && (h.MeetingID == nil || *h.MeetingID != mid) {
			t.Errorf("meeting/answer hit should point at meeting %d: %+v", mid, h)
		}
	}
}

func TestSearch_PrefixHidesVoidedAndSurvivesOperators(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	keep := mustEntry(t, s, newEntry(p, nil, "Kubernetes upskilling"))
	gone := mustEntry(t, s, newEntry(p, nil, "Kubernetes mistake"))
	s.FinalizeEntry(ctx, keep)
	s.FinalizeEntry(ctx, gone)
	s.VoidEntry(ctx, gone, "wrong person")

	hits, err := s.Search(ctx, "kuber", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != keep || hits[0].Draft {
		t.Errorf("prefix search hits = %+v, want only the live, finalized entry", hits)
	}

	for _, q := range []string{`"`, `AND OR`, `(((`, `*`, `a:b`, ""} {
		if _, err := s.Search(ctx, q, 20); err != nil {
			t.Errorf("Search(%q) must never fail on odd input: %v", q, err)
		}
	}
}

func TestPendingDrafts(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	r := mustRelease(t, s, "R1")
	mustMeeting(t, s, p)
	e := mustEntry(t, s, newEntry(p, nil, "a"))
	mustEntry(t, s, newEntry(p, nil, "b"))
	mustCommitment(t, s, newCommitment(p, nil, "c"))
	mustWorkItem(t, s, newWorkItem(p, r, "w", ""))
	s.FinalizeEntry(ctx, e)

	got, err := s.PendingDrafts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := DraftCounts{Meetings: 1, Entries: 1, Commitments: 1, WorkItems: 1}
	if got != want {
		t.Errorf("PendingDrafts() = %+v, want %+v", got, want)
	}
}

func TestProgressStatuses(t *testing.T) {
	s := newTestStore(t)
	onTime := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	stale := mustCreatePerson(t, s, "Bia", domain.RelationshipReport)
	mustCreatePerson(t, s, "Cris", domain.RelationshipReport) // never checked
	mustCreatePerson(t, s, "Boss", domain.RelationshipManager)

	check := func(p int64, when time.Time, o domain.Outcome) {
		e := newEntry(p, nil, "check")
		e.Kind, e.Outcome, e.OccurredAt = "progress_check", o, when
		s.FinalizeEntry(ctx, mustEntry(t, s, e))
	}
	now := day(2024, 6, 15)
	check(onTime, day(2024, 6, 10), domain.OutcomeProgress)
	check(stale, day(2024, 4, 1), domain.OutcomeNoProgress)
	check(stale, day(2024, 5, 1), domain.OutcomeNoProgress)

	got, err := s.ProgressStatuses(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("statuses = %d, want only the 3 reports", len(got))
	}
	byName := map[string]PersonProgress{}
	for _, pp := range got {
		byName[pp.Person.Name] = pp
	}
	if a := byName["Ana"]; a.Overdue || a.NoProgressStreak != 0 || a.LastCheck == nil {
		t.Errorf("Ana = %+v, want on time", a)
	}
	if b := byName["Bia"]; !b.Overdue || b.NoProgressStreak != 2 {
		t.Errorf("Bia = %+v, want overdue with a streak of 2", b)
	}
	if c := byName["Cris"]; !c.Overdue || c.LastCheck != nil {
		t.Errorf("Cris = %+v, want overdue (never checked)", c)
	}
}

func TestReleaseCompliance(t *testing.T) {
	s := newTestStore(t)
	ana := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	bia := mustCreatePerson(t, s, "Bia", domain.RelationshipReport)
	cris := mustCreatePerson(t, s, "Cris", domain.RelationshipReport)
	r := mustRelease(t, s, "R1")

	finalize := func(w domain.WorkItem) {
		s.FinalizeWorkItem(ctx, mustWorkItem(t, s, w))
	}
	finalize(newWorkItem(ana, r, "Stretch task", "P-1"))
	atLevel := newWorkItem(bia, r, "Routine task", "P-2")
	atLevel.Stretch = domain.StretchAt
	finalize(atLevel)
	mustWorkItem(t, s, newWorkItem(cris, r, "Still a draft", "P-3")) // drafts do not count

	got, err := s.ReleaseCompliance(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("rows = %d, want one per report", len(got))
	}
	want := map[string]bool{"Ana": true, "Bia": false, "Cris": false}
	for _, row := range got {
		if row.Compliant != want[row.Person.Name] {
			t.Errorf("%s compliant = %v, want %v", row.Person.Name, row.Compliant, want[row.Person.Name])
		}
	}
	if _, err := s.ReleaseCompliance(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown release error = %v, want ErrNotFound", err)
	}
}
