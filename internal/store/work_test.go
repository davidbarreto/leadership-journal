package store

import (
	"errors"
	"testing"
	"time"

	"leadership-journal/internal/domain"
)

func mustRelease(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	id, err := s.CreateRelease(ctx, domain.Release{Name: name, StartsOn: day(2024, 1, 1), EndsOn: day(2024, 3, 31)})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newWorkItem(person, release int64, title, ticket string) domain.WorkItem {
	return domain.WorkItem{
		PersonID:         person,
		ReleaseID:        release,
		Title:            title,
		ExternalRef:      ticket,
		PickedBy:         domain.PickedByLead,
		Stretch:          domain.StretchAbove,
		StretchRationale: "first time in billing",
		AssignedAt:       day(2024, 1, 15),
	}
}

func mustWorkItem(t *testing.T, s *Store, w domain.WorkItem) int64 {
	t.Helper()
	id, err := s.CreateWorkItem(ctx, w)
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}
	return id
}

func TestWorkItem_CreateRoundTrip(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	r := mustRelease(t, s, "R1")

	id := mustWorkItem(t, s, newWorkItem(p, r, "Migrate billing", "PROJ-1"))
	got, err := s.GetWorkItem(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Migrate billing" || got.ExternalRef != "PROJ-1" || got.Stretch != domain.StretchAbove ||
		got.PickedBy != domain.PickedByLead || got.StretchRationale != "first time in billing" {
		t.Errorf("work item = %+v", got)
	}
	if got.Status != domain.WorkPlanned {
		t.Errorf("default status = %q, want planned", got.Status)
	}
	if !got.AssignedAt.Equal(day(2024, 1, 15)) || got.CompletedAt != nil || got.MovedFromID != nil || !got.IsDraft() {
		t.Errorf("work item = %+v", got)
	}
	if _, err := s.GetWorkItem(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing work item error = %v", err)
	}
}

func TestWorkItem_ValidationAndDefaults(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	r := mustRelease(t, s, "R1")

	bad := newWorkItem(p, r, "", "")
	bad.Stretch = "huge"
	if _, err := s.CreateWorkItem(ctx, bad); !errors.Is(err, domain.ErrTitleRequired) || !errors.Is(err, domain.ErrInvalidStretch) {
		t.Errorf("invalid item error = %v", err)
	}

	noDate := newWorkItem(p, r, "Defaults", "")
	noDate.AssignedAt = time.Time{}
	id := mustWorkItem(t, s, noDate)
	got, _ := s.GetWorkItem(ctx, id)
	if !got.AssignedAt.Equal(testClock()) {
		t.Errorf("AssignedAt default = %v, want the store clock", got.AssignedAt)
	}
}

func TestWorkItem_TicketsAreUnique_BlanksAreNot(t *testing.T) {
	s := newTestStore(t)
	ana := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	bia := mustCreatePerson(t, s, "Bia", domain.RelationshipReport)
	r := mustRelease(t, s, "R1")

	mustWorkItem(t, s, newWorkItem(ana, r, "A", "PROJ-1"))
	if _, err := s.CreateWorkItem(ctx, newWorkItem(bia, r, "B", "PROJ-1")); !errors.Is(err, ErrDuplicate) {
		t.Errorf("same ticket twice error = %v, want ErrDuplicate", err)
	}
	mustWorkItem(t, s, newWorkItem(ana, r, "No ticket 1", ""))
	mustWorkItem(t, s, newWorkItem(ana, r, "No ticket 2", "  "))
}

func TestWorkItem_JudgmentIsEditableOnlyWhileDraft(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	r := mustRelease(t, s, "R1")
	id := mustWorkItem(t, s, newWorkItem(p, r, "Migrate billing", "PROJ-1"))

	w, _ := s.GetWorkItem(ctx, id)
	w.Stretch, w.StretchRationale = domain.StretchAt, "they did this last year"
	if err := s.UpdateWorkItem(ctx, w); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetWorkItem(ctx, id)
	if got.Stretch != domain.StretchAt {
		t.Errorf("draft update not applied: %+v", got)
	}

	if err := s.FinalizeWorkItem(ctx, id); err != nil {
		t.Fatal(err)
	}
	w.Stretch = domain.StretchBelow
	if err := s.UpdateWorkItem(ctx, w); !errors.Is(err, ErrNotDraft) {
		t.Errorf("editing the judgment after finalize error = %v, want ErrNotDraft", err)
	}
}

func TestWorkItem_TrackingChangesAfterFinalize(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	r := mustRelease(t, s, "R1")
	id := mustWorkItem(t, s, newWorkItem(p, r, "Migrate billing", "PROJ-1"))
	if err := s.FinalizeWorkItem(ctx, id); err != nil {
		t.Fatal(err)
	}

	err := s.UpdateWorkTracking(ctx, id, WorkTracking{
		Status: domain.WorkInProgress, InvestigationDone: true, InvestigationURL: "https://wiki/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetWorkItem(ctx, id)
	if got.Status != domain.WorkInProgress || !got.InvestigationDone || got.InvestigationURL != "https://wiki/x" || got.CompletedAt != nil {
		t.Errorf("after tracking update = %+v", got)
	}

	if err := s.UpdateWorkTracking(ctx, id, WorkTracking{Status: domain.WorkDone, OutcomeNotes: "Worked independently"}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetWorkItem(ctx, id)
	if got.Status != domain.WorkDone || got.CompletedAt == nil || !got.CompletedAt.Equal(testClock()) || got.OutcomeNotes != "Worked independently" {
		t.Errorf("after done = %+v (completion should default to now)", got)
	}

	if err := s.UpdateWorkTracking(ctx, id, WorkTracking{Status: domain.WorkInProgress}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetWorkItem(ctx, id)
	if got.CompletedAt != nil {
		t.Errorf("reopening must clear the completion date, got %v", got.CompletedAt)
	}

	if err := s.UpdateWorkTracking(ctx, id, WorkTracking{Status: domain.WorkMoved}); !errors.Is(err, domain.ErrInvalidWorkStatus) {
		t.Errorf("setting moved directly error = %v, want ErrInvalidWorkStatus", err)
	}
	if err := s.UpdateWorkTracking(ctx, 999, WorkTracking{Status: domain.WorkDone}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing item error = %v", err)
	}
}

func TestWorkItem_MoveKeepsTheOriginalAndCreatesAnother(t *testing.T) {
	s := newTestStore(t)
	ana := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	bia := mustCreatePerson(t, s, "Bia", domain.RelationshipReport)
	r := mustRelease(t, s, "R1")
	orig := mustWorkItem(t, s, newWorkItem(ana, r, "Migrate billing", "PROJ-1"))
	s.FinalizeWorkItem(ctx, orig)

	newID, err := s.MoveWorkItem(ctx, orig, domain.WorkItem{
		PersonID: bia, PickedBy: domain.PickedByLead, Stretch: domain.StretchAt,
		StretchRationale: "Bia knows billing", AssignedAt: day(2024, 2, 1),
	})
	if err != nil {
		t.Fatal(err)
	}

	before, _ := s.GetWorkItem(ctx, orig)
	after, _ := s.GetWorkItem(ctx, newID)
	if before.Status != domain.WorkMoved || before.PersonID != ana || before.Stretch != domain.StretchAbove {
		t.Errorf("original = %+v, want moved with its judgment untouched", before)
	}
	if after.PersonID != bia || after.MovedFromID == nil || *after.MovedFromID != orig ||
		after.Title != "Migrate billing" || after.ExternalRef != "PROJ-1" || after.ReleaseID != r ||
		after.Stretch != domain.StretchAt || after.Status != domain.WorkPlanned || !after.IsDraft() {
		t.Errorf("new item = %+v", after)
	}

	if _, err := s.MoveWorkItem(ctx, orig, domain.WorkItem{PersonID: ana, PickedBy: domain.PickedByLead, Stretch: domain.StretchAt, StretchRationale: "x"}); !errors.Is(err, ErrCannotMove) {
		t.Errorf("moving an already moved item error = %v, want ErrCannotMove", err)
	}
}

func TestWorkItem_MoveRules(t *testing.T) {
	s := newTestStore(t)
	ana := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	bia := mustCreatePerson(t, s, "Bia", domain.RelationshipReport)
	r := mustRelease(t, s, "R1")
	to := domain.WorkItem{PersonID: bia, PickedBy: domain.PickedByLead, Stretch: domain.StretchAt, StretchRationale: "x"}

	done := mustWorkItem(t, s, newWorkItem(ana, r, "Done already", "PROJ-2"))
	s.UpdateWorkTracking(ctx, done, WorkTracking{Status: domain.WorkDone})
	if _, err := s.MoveWorkItem(ctx, done, to); !errors.Is(err, ErrCannotMove) {
		t.Errorf("moving a done task error = %v, want ErrCannotMove", err)
	}

	active := mustWorkItem(t, s, newWorkItem(ana, r, "Active", "PROJ-3"))
	same := to
	same.PersonID = ana
	if _, err := s.MoveWorkItem(ctx, active, same); !errors.Is(err, ErrCannotMove) {
		t.Errorf("moving to the same person error = %v, want ErrCannotMove", err)
	}

	noRationale := to
	noRationale.StretchRationale = ""
	if _, err := s.MoveWorkItem(ctx, active, noRationale); !errors.Is(err, domain.ErrRationaleRequired) {
		t.Errorf("move without a new judgment error = %v, want ErrRationaleRequired", err)
	}
	got, _ := s.GetWorkItem(ctx, active)
	if got.Status != domain.WorkPlanned {
		t.Errorf("a failed move must leave the original untouched, status = %q", got.Status)
	}
	if _, err := s.MoveWorkItem(ctx, 999, to); !errors.Is(err, ErrNotFound) {
		t.Errorf("moving a missing item error = %v", err)
	}
}

func TestWorkItem_ListFiltersAndLifecycle(t *testing.T) {
	s := newTestStore(t)
	ana := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	bia := mustCreatePerson(t, s, "Bia", domain.RelationshipReport)
	r1 := mustRelease(t, s, "R1")
	r2 := mustRelease(t, s, "R2")

	a := mustWorkItem(t, s, newWorkItem(ana, r1, "A", "P-1"))
	mustWorkItem(t, s, newWorkItem(ana, r2, "B", "P-2"))
	mustWorkItem(t, s, newWorkItem(bia, r1, "C", "P-3"))

	got, _ := s.ListWorkItems(ctx, WorkFilter{PersonID: &ana})
	if len(got) != 2 {
		t.Errorf("items for Ana = %d, want 2", len(got))
	}
	got, _ = s.ListWorkItems(ctx, WorkFilter{PersonID: &ana, ReleaseID: &r1})
	if len(got) != 1 || got[0].ID != a {
		t.Errorf("items for Ana in R1 = %+v", got)
	}

	s.FinalizeWorkItem(ctx, a)
	if err := s.VoidWorkItem(ctx, a, "entered twice"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ListWorkItems(ctx, WorkFilter{PersonID: &ana, ReleaseID: &r1})
	if len(got) != 0 {
		t.Errorf("voided items must be hidden, got %d", len(got))
	}
	// A voided item frees its ticket for re-entry.
	mustWorkItem(t, s, newWorkItem(ana, r1, "A again", "P-1"))

	draft := mustWorkItem(t, s, newWorkItem(ana, r1, "scratch", "P-9"))
	if err := s.DeleteDraftWorkItem(ctx, draft); err != nil {
		t.Fatal(err)
	}
}
