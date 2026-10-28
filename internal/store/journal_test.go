package store

import (
	"errors"
	"testing"
	"time"

	"leadership-journal/internal/domain"
)

func saoPaulo(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func newMeeting(t *testing.T, person int64) domain.Meeting {
	t.Helper()
	return domain.Meeting{
		Kind:     "one_on_one",
		Title:    "Biweekly 1:1",
		PersonID: &person,
		StartsAt: time.Date(2024, 3, 5, 14, 0, 0, 0, saoPaulo(t)),
		Notes:    "Talked about the migration",
		Answers: []domain.Answer{
			{PromptKey: "learned", PromptText: "What did you learn?", Text: "Go interfaces"},
			{PromptKey: "applied", PromptText: "How did you apply it?", Text: ""},
		},
	}
}

func mustMeeting(t *testing.T, s *Store, person int64) int64 {
	t.Helper()
	id, err := s.CreateMeeting(ctx, newMeeting(t, person))
	if err != nil {
		t.Fatalf("CreateMeeting: %v", err)
	}
	return id
}

func newEntry(person int64, meeting *int64, body string) domain.Entry {
	return domain.Entry{
		PersonID:   person,
		MeetingID:  meeting,
		Kind:       "observation",
		Body:       body,
		OccurredAt: time.Date(2024, 3, 5, 14, 30, 0, 0, time.UTC),
	}
}

func mustEntry(t *testing.T, s *Store, e domain.Entry) int64 {
	t.Helper()
	id, err := s.CreateEntry(ctx, e)
	if err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	return id
}

func newCommitment(person int64, meeting *int64, content string) domain.Commitment {
	return domain.Commitment{
		PersonID:  person,
		MeetingID: meeting,
		Kind:      domain.CommitmentTask,
		Direction: domain.TheyOweMe,
		Content:   content,
	}
}

func mustCommitment(t *testing.T, s *Store, c domain.Commitment) int64 {
	t.Helper()
	id, err := s.CreateCommitment(ctx, c)
	if err != nil {
		t.Fatalf("CreateCommitment: %v", err)
	}
	return id
}

func TestMeeting_RoundTripKeepsTimeZoneAndAnswers(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	in := newMeeting(t, p)
	id, err := s.CreateMeeting(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetMeeting(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != in.Title || got.Notes != in.Notes || got.Kind != "one_on_one" || got.PersonID == nil || *got.PersonID != p {
		t.Errorf("meeting = %+v", got)
	}
	if !got.StartsAt.Equal(in.StartsAt) || got.StartsAt.Location().String() != "America/Sao_Paulo" {
		t.Errorf("StartsAt = %v in %v, want %v in America/Sao_Paulo", got.StartsAt, got.StartsAt.Location(), in.StartsAt)
	}
	if got.CreatedAt.Location().String() != "Europe/Lisbon" || !got.CreatedAt.Equal(testClock()) {
		t.Errorf("CreatedAt = %v (%v), want the store clock in Europe/Lisbon", got.CreatedAt, got.CreatedAt.Location())
	}
	if !got.IsDraft() {
		t.Errorf("a new meeting must be a draft")
	}
	if len(got.Answers) != 1 || got.Answers[0].PromptKey != "learned" || got.Answers[0].Text != "Go interfaces" ||
		got.Answers[0].PromptText != "What did you learn?" {
		t.Errorf("answers = %+v, want only the non-empty one with its prompt wording", got.Answers)
	}
	if _, err := s.GetMeeting(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing meeting error = %v", err)
	}
}

func TestMeeting_Validation(t *testing.T) {
	s := newTestStore(t)
	m := newMeeting(t, 1)
	m.PersonID = nil
	if _, err := s.CreateMeeting(ctx, m); !errors.Is(err, domain.ErrPersonRequired) {
		t.Errorf("1:1 without person error = %v, want ErrPersonRequired", err)
	}
	m.Kind = "nope"
	if _, err := s.CreateMeeting(ctx, m); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown kind error = %v, want ErrNotFound", err)
	}

	team := domain.Meeting{Kind: "team", Title: "Planning", StartsAt: testClock()}
	if _, err := s.CreateMeeting(ctx, team); err != nil {
		t.Errorf("team meeting without person should be fine: %v", err)
	}
}

func TestMeeting_UpdateOnlyWhileDraft(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	id := mustMeeting(t, s, p)

	m, _ := s.GetMeeting(ctx, id)
	m.Notes = "Revised notes"
	m.Answers = []domain.Answer{{PromptKey: "applied", PromptText: "How did you apply it?", Text: "In the billing job"}}
	if err := s.UpdateMeeting(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetMeeting(ctx, id)
	if got.Notes != "Revised notes" || len(got.Answers) != 1 || got.Answers[0].PromptKey != "applied" {
		t.Errorf("after update = %+v", got)
	}

	if err := s.FinalizeMeeting(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMeeting(ctx, m); !errors.Is(err, ErrNotDraft) {
		t.Errorf("update after finalize error = %v, want ErrNotDraft", err)
	}
	if err := s.FinalizeMeeting(ctx, id); !errors.Is(err, ErrNotDraft) {
		t.Errorf("finalizing twice error = %v, want ErrNotDraft", err)
	}
}

func TestMeeting_FinalizeLocksEverythingCreatedInIt(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	mid := mustMeeting(t, s, p)
	eid := mustEntry(t, s, newEntry(p, &mid, "Led the review"))
	cid := mustCommitment(t, s, newCommitment(p, &mid, "Write the runbook"))

	if err := s.FinalizeMeeting(ctx, mid); err != nil {
		t.Fatal(err)
	}

	e, _ := s.GetEntry(ctx, eid)
	c, _ := s.GetCommitment(ctx, cid)
	if !e.IsFinalized() || !c.IsFinalized() {
		t.Errorf("entry finalized = %v, commitment finalized = %v, want both true", e.IsFinalized(), c.IsFinalized())
	}
	e.Body = "rewritten"
	if err := s.UpdateEntry(ctx, e); !errors.Is(err, ErrNotDraft) {
		t.Errorf("updating a locked entry error = %v, want ErrNotDraft", err)
	}
	if _, err := s.CreateEntry(ctx, newEntry(p, &mid, "late addition")); !errors.Is(err, ErrNotDraft) {
		t.Errorf("adding to a finalized meeting error = %v, want ErrNotDraft", err)
	}
	if err := s.DeleteDraftEntry(ctx, eid); !errors.Is(err, ErrNotDraft) {
		t.Errorf("deleting a locked entry error = %v, want ErrNotDraft", err)
	}
}

func TestMeeting_VoidNeedsReasonAndCascades(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	mid := mustMeeting(t, s, p)
	eid := mustEntry(t, s, newEntry(p, &mid, "Led the review"))
	cid := mustCommitment(t, s, newCommitment(p, &mid, "Write the runbook"))

	if err := s.VoidMeeting(ctx, mid, "x"); !errors.Is(err, ErrNotFinalized) {
		t.Errorf("voiding a draft error = %v, want ErrNotFinalized", err)
	}
	if err := s.FinalizeMeeting(ctx, mid); err != nil {
		t.Fatal(err)
	}
	if err := s.VoidMeeting(ctx, mid, "  "); !errors.Is(err, ErrReasonRequired) {
		t.Errorf("void without reason error = %v, want ErrReasonRequired", err)
	}
	if err := s.VoidMeeting(ctx, mid, "Wrong person"); err != nil {
		t.Fatal(err)
	}

	m, _ := s.GetMeeting(ctx, mid)
	e, _ := s.GetEntry(ctx, eid)
	c, _ := s.GetCommitment(ctx, cid)
	if !m.IsVoided() || m.VoidReason != "Wrong person" || !e.IsVoided() || !c.IsVoided() {
		t.Errorf("voided = meeting %v entry %v commitment %v, want all true", m.IsVoided(), e.IsVoided(), c.IsVoided())
	}
	if err := s.VoidMeeting(ctx, mid, "again"); !errors.Is(err, ErrNotFinalized) {
		t.Errorf("voiding twice error = %v, want ErrNotFinalized", err)
	}
}

func TestMeeting_DeleteDraftRemovesEverything(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	mid := mustMeeting(t, s, p)
	e := newEntry(p, &mid, "Led the review")
	e.Tags = []string{"ownership"}
	mustEntry(t, s, e)
	mustCommitment(t, s, newCommitment(p, &mid, "Write the runbook"))

	if err := s.DeleteDraftMeeting(ctx, mid); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"meeting", "meeting_answer", "entry", "entry_tag", "commitment", "commitment_event"} {
		if n := count(t, s, `SELECT COUNT(*) FROM `+table); n != 0 {
			t.Errorf("%s still has %d rows after deleting the draft meeting", table, n)
		}
	}
	if n := count(t, s, `SELECT COUNT(*) FROM search_index`); n != 0 {
		t.Errorf("search index still has %d rows", n)
	}

	locked := mustMeeting(t, s, p)
	if err := s.FinalizeMeeting(ctx, locked); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDraftMeeting(ctx, locked); !errors.Is(err, ErrNotDraft) {
		t.Errorf("deleting a finalized meeting error = %v, want ErrNotDraft", err)
	}
	if err := s.DeleteDraftMeeting(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting a missing meeting error = %v, want ErrNotFound", err)
	}
}

func TestMeeting_ListFiltersAndOrders(t *testing.T) {
	s := newTestStore(t)
	ana := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	bia := mustCreatePerson(t, s, "Bia", domain.RelationshipReport)

	old := newMeeting(t, ana)
	old.StartsAt = old.StartsAt.AddDate(0, 0, -14)
	oldID, _ := s.CreateMeeting(ctx, old)
	newID := mustMeeting(t, s, ana)
	mustMeeting(t, s, bia)

	got, err := s.ListMeetings(ctx, MeetingFilter{PersonID: &ana})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != newID || got[1].ID != oldID {
		t.Errorf("meetings for Ana = %+v, want newest first", got)
	}

	s.FinalizeMeeting(ctx, oldID)
	s.VoidMeeting(ctx, oldID, "duplicate")
	got, _ = s.ListMeetings(ctx, MeetingFilter{PersonID: &ana})
	if len(got) != 1 {
		t.Errorf("voided meetings must be hidden by default, got %d", len(got))
	}
	got, _ = s.ListMeetings(ctx, MeetingFilter{PersonID: &ana, IncludeVoided: true})
	if len(got) != 2 {
		t.Errorf("IncludeVoided should show both, got %d", len(got))
	}
	got, _ = s.ListMeetings(ctx, MeetingFilter{Limit: 1})
	if len(got) != 1 {
		t.Errorf("Limit not applied, got %d", len(got))
	}
}

func TestEntry_CreateValidatesAgainstItsKind(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)

	check := newEntry(p, nil, "Applied what they learned")
	check.Kind = "progress_check"
	if _, err := s.CreateEntry(ctx, check); !errors.Is(err, domain.ErrOutcomeRequired) {
		t.Errorf("progress check without outcome error = %v", err)
	}
	check.Outcome = domain.OutcomeProgress
	if _, err := s.CreateEntry(ctx, check); err != nil {
		t.Errorf("valid progress check: %v", err)
	}

	unknown := newEntry(p, nil, "x")
	unknown.Kind = "nope"
	if _, err := s.CreateEntry(ctx, unknown); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown kind error = %v, want ErrNotFound", err)
	}
}

func TestEntry_TagsAreNormalizedAndReplaced(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	in := newEntry(p, nil, "Wrote the incident runbook")
	in.Tags = []string{"Documentation", " ownership ", "documentation", "new-topic", ""}
	id := mustEntry(t, s, in)

	got, err := s.GetEntry(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 3 || got.Tags[0] != "documentation" || got.Tags[1] != "new-topic" || got.Tags[2] != "ownership" {
		t.Errorf("tags = %v, want deduplicated, lowercase and sorted", got.Tags)
	}
	tags, _ := s.ListTags(ctx)
	found := false
	for _, tag := range tags {
		found = found || tag == "new-topic"
	}
	if !found {
		t.Errorf("new tag was not added to the tag list: %v", tags)
	}

	got.Tags = []string{"communication"}
	got.Body = "Edited"
	if err := s.UpdateEntry(ctx, got); err != nil {
		t.Fatal(err)
	}
	after, _ := s.GetEntry(ctx, id)
	if after.Body != "Edited" || len(after.Tags) != 1 || after.Tags[0] != "communication" {
		t.Errorf("after update = %+v", after)
	}
}

func TestEntry_StandaloneLifecycle(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	id := mustEntry(t, s, newEntry(p, nil, "Good code review"))

	if err := s.FinalizeEntry(ctx, id); err != nil {
		t.Fatal(err)
	}
	e, _ := s.GetEntry(ctx, id)
	if !e.IsFinalized() || e.FinalizedAt == nil || !e.FinalizedAt.Equal(testClock()) {
		t.Errorf("finalized entry = %+v", e.Lifecycle)
	}
	if err := s.VoidEntry(ctx, id, "Noted on the wrong person"); err != nil {
		t.Fatal(err)
	}
	e, _ = s.GetEntry(ctx, id)
	if !e.IsVoided() || e.VoidReason != "Noted on the wrong person" {
		t.Errorf("voided entry = %+v", e.Lifecycle)
	}

	draft := mustEntry(t, s, newEntry(p, nil, "scratch"))
	if err := s.DeleteDraftEntry(ctx, draft); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetEntry(ctx, draft); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted draft still readable: %v", err)
	}
}

func TestEntry_CorrectionPointsAtTheOriginal(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	orig := mustEntry(t, s, newEntry(p, nil, "Struggles with ownership"))
	s.FinalizeEntry(ctx, orig)

	fix := newEntry(p, nil, "Improving at ownership")
	fix.CorrectsID = &orig
	id := mustEntry(t, s, fix)
	got, _ := s.GetEntry(ctx, id)
	if got.CorrectsID == nil || *got.CorrectsID != orig {
		t.Errorf("CorrectsID = %v, want %d", got.CorrectsID, orig)
	}
}

func TestEntry_MeetingLinkedEntriesFinalizeWithTheirMeeting(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	mid := mustMeeting(t, s, p)
	id := mustEntry(t, s, newEntry(p, &mid, "Led the review"))
	if err := s.FinalizeEntry(ctx, id); !errors.Is(err, ErrFinalizeWithMeeting) {
		t.Errorf("finalizing a meeting entry alone error = %v, want ErrFinalizeWithMeeting", err)
	}
}

func TestEntry_ListFilters(t *testing.T) {
	s := newTestStore(t)
	ana := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	bia := mustCreatePerson(t, s, "Bia", domain.RelationshipReport)

	a := newEntry(ana, nil, "first")
	a.Tags = []string{"ownership"}
	a.OccurredAt = a.OccurredAt.AddDate(0, 0, -10)
	aID := mustEntry(t, s, a)

	b := newEntry(ana, nil, "second")
	b.Kind = "strength"
	bID := mustEntry(t, s, b)
	mustEntry(t, s, newEntry(bia, nil, "someone else"))
	s.FinalizeEntry(ctx, aID)

	all, _ := s.ListEntries(ctx, EntryFilter{PersonID: &ana})
	if len(all) != 2 || all[0].ID != bID || all[1].ID != aID {
		t.Errorf("entries for Ana = %+v, want newest occurrence first", all)
	}
	byKind, _ := s.ListEntries(ctx, EntryFilter{PersonID: &ana, Kind: "strength"})
	if len(byKind) != 1 || byKind[0].ID != bID {
		t.Errorf("by kind = %+v", byKind)
	}
	byTag, _ := s.ListEntries(ctx, EntryFilter{PersonID: &ana, Tag: "ownership"})
	if len(byTag) != 1 || byTag[0].ID != aID {
		t.Errorf("by tag = %+v", byTag)
	}
	finalized, _ := s.ListEntries(ctx, EntryFilter{PersonID: &ana, FinalizedOnly: true})
	if len(finalized) != 1 || finalized[0].ID != aID {
		t.Errorf("finalized only = %+v", finalized)
	}
	s.VoidEntry(ctx, aID, "wrong")
	visible, _ := s.ListEntries(ctx, EntryFilter{PersonID: &ana})
	if len(visible) != 1 {
		t.Errorf("voided entries must be hidden by default, got %d", len(visible))
	}
	withVoided, _ := s.ListEntries(ctx, EntryFilter{PersonID: &ana, IncludeVoided: true})
	if len(withVoided) != 2 {
		t.Errorf("IncludeVoided should show both, got %d", len(withVoided))
	}
}

func TestCommitment_StatusIsAnAppendOnlyLog(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	id := mustCommitment(t, s, newCommitment(p, nil, "Write the runbook"))

	c, _ := s.GetCommitment(ctx, id)
	if c.Status != domain.CommitmentOpen {
		t.Errorf("new commitment status = %q, want open", c.Status)
	}

	if err := s.SetCommitmentStatus(ctx, id, domain.CommitmentDone, "Presented at the guild"); err != nil {
		t.Fatal(err)
	}
	c, _ = s.GetCommitment(ctx, id)
	if c.Status != domain.CommitmentDone || c.StatusNote != "Presented at the guild" {
		t.Errorf("after done = %q / %q", c.Status, c.StatusNote)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM commitment_event WHERE commitment_id = ?`, id); n != 2 {
		t.Errorf("events = %d, want the initial open plus done", n)
	}

	if err := s.SetCommitmentStatus(ctx, id, "paused", ""); !errors.Is(err, domain.ErrInvalidCommitmentStatus) {
		t.Errorf("invalid status error = %v", err)
	}
	if err := s.SetCommitmentStatus(ctx, 999, domain.CommitmentDone, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing commitment error = %v", err)
	}
}

func TestCommitment_LifecycleAndValidation(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)

	learning := newCommitment(p, nil, "Learn Kubernetes")
	learning.Kind = domain.CommitmentLearning
	if _, err := s.CreateCommitment(ctx, learning); !errors.Is(err, domain.ErrDeliverableRequired) || !errors.Is(err, domain.ErrDueDateRequired) {
		t.Errorf("learning goal without deliverable/due error = %v", err)
	}
	due := day(2024, 5, 1)
	learning.Deliverable, learning.DueOn = "Demo to the team", &due
	id := mustCommitment(t, s, learning)
	got, _ := s.GetCommitment(ctx, id)
	if got.Deliverable != "Demo to the team" || got.DueOn == nil || !got.DueOn.Equal(due) {
		t.Errorf("learning goal = %+v", got)
	}

	if err := s.FinalizeCommitment(ctx, id); err != nil {
		t.Fatal(err)
	}
	// Status changes stay possible after locking: they are events, not edits.
	if err := s.SetCommitmentStatus(ctx, id, domain.CommitmentMissed, "Postponed twice"); err != nil {
		t.Errorf("status change after finalize: %v", err)
	}
	if err := s.VoidCommitment(ctx, id, "Duplicate"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCommitmentStatus(ctx, id, domain.CommitmentDone, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("status change on a voided commitment error = %v, want ErrNotFound", err)
	}

	draft := mustCommitment(t, s, newCommitment(p, nil, "scratch"))
	if err := s.DeleteDraftCommitment(ctx, draft); err != nil {
		t.Fatal(err)
	}
}

func TestCommitment_ListFilters(t *testing.T) {
	s := newTestStore(t)
	ana := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	bia := mustCreatePerson(t, s, "Bia", domain.RelationshipReport)
	openID := mustCommitment(t, s, newCommitment(ana, nil, "open one"))
	doneID := mustCommitment(t, s, newCommitment(ana, nil, "done one"))
	mustCommitment(t, s, newCommitment(bia, nil, "someone else"))
	s.SetCommitmentStatus(ctx, doneID, domain.CommitmentDone, "")

	all, _ := s.ListCommitments(ctx, CommitmentFilter{PersonID: &ana})
	if len(all) != 2 {
		t.Errorf("all for Ana = %d, want 2", len(all))
	}
	open, _ := s.ListCommitments(ctx, CommitmentFilter{PersonID: &ana, OnlyOpen: true})
	if len(open) != 1 || open[0].ID != openID {
		t.Errorf("open for Ana = %+v", open)
	}
	everyoneOpen, _ := s.ListCommitments(ctx, CommitmentFilter{OnlyOpen: true})
	if len(everyoneOpen) != 2 {
		t.Errorf("open across people = %d, want 2", len(everyoneOpen))
	}
}

func TestProgressChecks(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)

	if last, err := s.LastProgressCheck(ctx, p); err != nil || last != nil {
		t.Fatalf("no checks yet: last = %v, err = %v", last, err)
	}

	check := func(when time.Time, outcome domain.Outcome, finalize bool) {
		e := newEntry(p, nil, "check")
		e.Kind, e.Outcome, e.OccurredAt = "progress_check", outcome, when
		id := mustEntry(t, s, e)
		if finalize {
			if err := s.FinalizeEntry(ctx, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	check(day(2024, 1, 10), domain.OutcomeProgress, true)
	check(day(2024, 1, 24), domain.OutcomeNoProgress, true)
	check(day(2024, 2, 7), domain.OutcomeNoProgress, true)
	check(day(2024, 2, 21), domain.OutcomeProgress, false) // draft: must be ignored

	last, err := s.LastProgressCheck(ctx, p)
	if err != nil || last == nil || !last.Equal(day(2024, 2, 7)) {
		t.Errorf("LastProgressCheck = %v, %v; want 2024-02-07 (drafts ignored)", last, err)
	}
	outcomes, err := s.ProgressOutcomes(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Outcome{domain.OutcomeNoProgress, domain.OutcomeNoProgress, domain.OutcomeProgress}
	if len(outcomes) != 3 || outcomes[0] != want[0] || outcomes[1] != want[1] || outcomes[2] != want[2] {
		t.Errorf("outcomes = %v, want %v (newest first)", outcomes, want)
	}
	if domain.ConsecutiveNoProgress(outcomes) != 2 {
		t.Errorf("streak = %d, want 2", domain.ConsecutiveNoProgress(outcomes))
	}
}
