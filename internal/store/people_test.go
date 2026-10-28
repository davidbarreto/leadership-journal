package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"leadership-journal/internal/domain"
)

var ctx = context.Background()

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func mustCreatePerson(t *testing.T, s *Store, name string, rel domain.Relationship) int64 {
	t.Helper()
	id, err := s.CreatePerson(ctx, domain.Person{Name: name, Relationship: rel})
	if err != nil {
		t.Fatalf("CreatePerson(%s): %v", name, err)
	}
	return id
}

func levelID(t *testing.T, s *Store, code string) int64 {
	t.Helper()
	levels, err := s.ListLevels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range levels {
		if l.Code == code {
			return l.ID
		}
	}
	t.Fatalf("level %s not found", code)
	return 0
}

func TestTeams(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateTeam(ctx, "Payments"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTeam(ctx, "Checkout"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTeam(ctx, "Payments"); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate team error = %v, want ErrDuplicate", err)
	}
	if _, err := s.CreateTeam(ctx, "  "); !errors.Is(err, domain.ErrNameRequired) {
		t.Errorf("blank team error = %v, want ErrNameRequired", err)
	}

	teams, err := s.ListTeams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 2 || teams[0].Name != "Checkout" || teams[1].Name != "Payments" {
		t.Errorf("teams = %+v, want Checkout then Payments (sorted by name)", teams)
	}
}

func TestPeople_RoundTripAndValidation(t *testing.T) {
	s := newTestStore(t)
	teamID, _ := s.CreateTeam(ctx, "Payments")

	id, err := s.CreatePerson(ctx, domain.Person{Name: "Ana", Relationship: domain.RelationshipReport, RoleTitle: "Backend", TeamID: &teamID})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPerson(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Ana" || got.RoleTitle != "Backend" || got.TeamID == nil || *got.TeamID != teamID || got.ID != id {
		t.Errorf("GetPerson() = %+v", got)
	}

	if _, err := s.CreatePerson(ctx, domain.Person{Name: "", Relationship: "friend"}); !errors.Is(err, domain.ErrNameRequired) || !errors.Is(err, domain.ErrInvalidRelationship) {
		t.Errorf("invalid person error = %v", err)
	}
	if _, err := s.GetPerson(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing person error = %v, want ErrNotFound", err)
	}
}

func TestPeople_UpdateAndList(t *testing.T) {
	s := newTestStore(t)
	zed := mustCreatePerson(t, s, "Zed", domain.RelationshipReport)
	mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	mustCreatePerson(t, s, "Boss", domain.RelationshipManager)

	if err := s.UpdatePerson(ctx, domain.Person{ID: zed, Name: "Zed Z.", Relationship: domain.RelationshipReport, RoleTitle: "SRE"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePerson(ctx, domain.Person{ID: 999, Name: "Ghost", Relationship: domain.RelationshipReport}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update missing person error = %v, want ErrNotFound", err)
	}

	reports, err := s.ListPeople(ctx, domain.RelationshipReport)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 || reports[0].Name != "Ana" || reports[1].Name != "Zed Z." || reports[1].RoleTitle != "SRE" {
		t.Errorf("reports = %+v", reports)
	}

	everyone, _ := s.ListPeople(ctx, "")
	if len(everyone) != 3 {
		t.Errorf("everyone = %d people, want 3", len(everyone))
	}
}

func TestLevels_CreateAndList(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateLevel(ctx, domain.Level{Track: domain.TrackQA, Code: "QA1", Title: "Junior", Rank: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateLevel(ctx, domain.Level{Track: domain.TrackQA, Code: "QA1", Title: "Again", Rank: 20}); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate level code error = %v, want ErrDuplicate", err)
	}
	if _, err := s.CreateLevel(ctx, domain.Level{Track: domain.TrackQA, Code: "QA9", Title: "", Rank: 10}); !errors.Is(err, domain.ErrTitleRequired) {
		t.Errorf("invalid level error = %v", err)
	}

	levels, err := s.ListLevels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 11 {
		t.Fatalf("levels = %d, want 10 seeded + 1 created", len(levels))
	}
	// Ordered by track then rank so a dropdown reads naturally.
	for i := 1; i < len(levels); i++ {
		a, b := levels[i-1], levels[i]
		if a.Track > b.Track || (a.Track == b.Track && a.Rank > b.Rank) {
			t.Errorf("levels not ordered by track and rank: %s before %s", a.Code, b.Code)
		}
	}
}

func TestAssignLevelEnforcesTheHistoryRules(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	e3, e4, e5 := levelID(t, s, "E3"), levelID(t, s, "E4"), levelID(t, s, "E5")

	if err := s.AssignLevel(ctx, p, e3, day(2023, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignLevel(ctx, p, e5, day(2024, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignLevel(ctx, p, e3, day(2025, 1, 1)); !errors.Is(err, domain.ErrLevelAlreadyHeld) {
		t.Errorf("repeat level error = %v, want ErrLevelAlreadyHeld", err)
	}
	// E4 after E5 would be a demotion, which the rules reject.
	if err := s.AssignLevel(ctx, p, e4, day(2025, 1, 1)); !errors.Is(err, domain.ErrRankNotIncreasing) {
		t.Errorf("demotion error = %v, want ErrRankNotIncreasing", err)
	}

	h, err := s.LevelHistory(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 2 {
		t.Fatalf("history length = %d, want 2 (rejected assignments must not persist)", len(h))
	}
	at, err := h.LevelAt(day(2023, 6, 1))
	if err != nil || at.Code != "E3" {
		t.Errorf("LevelAt(2023-06) = %v, %v; want E3", at.Code, err)
	}
	at, err = h.LevelAt(day(2024, 6, 1))
	if err != nil || at.Code != "E5" {
		t.Errorf("LevelAt(2024-06) = %v, %v; want E5", at.Code, err)
	}
}

func TestAssignLevel_UnknownPersonOrLevel(t *testing.T) {
	s := newTestStore(t)
	p := mustCreatePerson(t, s, "Ana", domain.RelationshipReport)
	if err := s.AssignLevel(ctx, 999, levelID(t, s, "E3"), day(2023, 1, 1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown person error = %v, want ErrNotFound", err)
	}
	if err := s.AssignLevel(ctx, p, 999, day(2023, 1, 1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown level error = %v, want ErrNotFound", err)
	}
}
