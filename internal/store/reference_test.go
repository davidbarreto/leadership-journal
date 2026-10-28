package store

import (
	"errors"
	"testing"

	"leadership-journal/internal/domain"
)

func TestMeetingKinds(t *testing.T) {
	s := newTestStore(t)
	k, err := s.CreateMeetingKind(ctx, "Skip-level", true)
	if err != nil {
		t.Fatal(err)
	}
	if k.Code != "skip_level" || !k.RequiresPerson {
		t.Errorf("created kind = %+v", k)
	}
	if _, err := s.CreateMeetingKind(ctx, "Skip level", false); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate code error = %v, want ErrDuplicate", err)
	}
	if _, err := s.CreateMeetingKind(ctx, "!!!", false); !errors.Is(err, domain.ErrCodeRequired) {
		t.Errorf("unusable label error = %v, want ErrCodeRequired", err)
	}

	got, err := s.GetMeetingKind(ctx, "one_on_one")
	if err != nil || !got.RequiresPerson {
		t.Errorf("GetMeetingKind(one_on_one) = %+v, %v", got, err)
	}
	if _, err := s.GetMeetingKind(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown kind error = %v, want ErrNotFound", err)
	}

	kinds, _ := s.ListMeetingKinds(ctx)
	if len(kinds) != 5 || kinds[0].Code != "one_on_one" || kinds[4].Code != "skip_level" {
		t.Errorf("kinds = %+v, want seeds first and the new kind last", kinds)
	}
}

func TestEntryKinds(t *testing.T) {
	s := newTestStore(t)
	k, err := s.CreateEntryKind(ctx, "Kudos", false)
	if err != nil {
		t.Fatal(err)
	}
	if k.Code != "kudos" {
		t.Errorf("code = %q", k.Code)
	}
	if _, err := s.CreateEntryKind(ctx, "Kudos", false); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate error = %v", err)
	}
	check, err := s.GetEntryKind(ctx, "progress_check")
	if err != nil || !check.RequiresOutcome {
		t.Errorf("progress_check = %+v, %v", check, err)
	}
	if _, err := s.GetEntryKind(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown kind error = %v", err)
	}
	kinds, _ := s.ListEntryKinds(ctx)
	if len(kinds) != 8 || kinds[7].Code != "kudos" {
		t.Errorf("entry kinds = %d (last %q), want the 7 seeds plus kudos last", len(kinds), kinds[len(kinds)-1].Code)
	}
}

func TestPrompts(t *testing.T) {
	s := newTestStore(t)
	seeded, err := s.ListPrompts(ctx, "one_on_one")
	if err != nil {
		t.Fatal(err)
	}
	if len(seeded) != 4 || seeded[0].Key != "learned" || seeded[1].Key != "applied" {
		t.Errorf("seeded 1:1 prompts = %+v", seeded)
	}

	p, err := s.CreatePrompt(ctx, "one_on_one", "How is your workload?")
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != "how_is_your_workload" || p.MeetingKind != "one_on_one" {
		t.Errorf("created prompt = %+v", p)
	}
	if _, err := s.CreatePrompt(ctx, "one_on_one", "How is your workload?"); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate prompt error = %v", err)
	}
	if _, err := s.CreatePrompt(ctx, "nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown kind error = %v, want ErrNotFound", err)
	}

	after, _ := s.ListPrompts(ctx, "one_on_one")
	if len(after) != 5 || after[4].Key != "how_is_your_workload" {
		t.Errorf("prompts after create = %+v", after)
	}
}

func TestTags(t *testing.T) {
	s := newTestStore(t)
	tags, err := s.ListTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 4 || tags[0] != "communication" {
		t.Errorf("seeded tags = %v, want 4 sorted by name", tags)
	}
}

func TestReleases(t *testing.T) {
	s := newTestStore(t)
	older, err := s.CreateRelease(ctx, domain.Release{Name: "R1", StartsOn: day(2024, 1, 1), EndsOn: day(2024, 3, 31)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRelease(ctx, domain.Release{Name: "R2", StartsOn: day(2024, 4, 1), EndsOn: day(2024, 6, 30)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRelease(ctx, domain.Release{Name: "Bad", StartsOn: day(2024, 4, 1), EndsOn: day(2024, 3, 1)}); !errors.Is(err, domain.ErrInvalidPeriod) {
		t.Errorf("invalid release error = %v", err)
	}

	releases, _ := s.ListReleases(ctx)
	if len(releases) != 2 || releases[0].Name != "R2" {
		t.Errorf("releases = %+v, want newest first", releases)
	}
	got, err := s.GetRelease(ctx, older)
	if err != nil || got.Name != "R1" || !got.StartsOn.Equal(day(2024, 1, 1)) || !got.EndsOn.Equal(day(2024, 3, 31)) {
		t.Errorf("GetRelease() = %+v, %v", got, err)
	}
	if _, err := s.GetRelease(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing release error = %v", err)
	}
}
