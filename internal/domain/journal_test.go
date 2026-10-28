package domain

import (
	"testing"
	"time"
)

func TestCommitment_Validate(t *testing.T) {
	valid := func() Commitment {
		return Commitment{
			PersonID:  1,
			Kind:      CommitmentTask,
			Direction: TheyOweMe,
			Content:   "Write the runbook",
			Status:    CommitmentOpen,
		}
	}
	due := day(2024, 5, 1)

	tests := []struct {
		name    string
		mutate  func(*Commitment)
		wantErr []error
	}{
		{name: "valid task without due date", mutate: func(*Commitment) {}},
		{name: "blank content", mutate: func(c *Commitment) { c.Content = " " }, wantErr: []error{ErrContentRequired}},
		{name: "missing person", mutate: func(c *Commitment) { c.PersonID = 0 }, wantErr: []error{ErrPersonRequired}},
		{name: "invalid kind", mutate: func(c *Commitment) { c.Kind = "wish" }, wantErr: []error{ErrInvalidCommitmentKind}},
		{name: "invalid direction", mutate: func(c *Commitment) { c.Direction = "x" }, wantErr: []error{ErrInvalidDirection}},
		{name: "invalid status", mutate: func(c *Commitment) { c.Status = "x" }, wantErr: []error{ErrInvalidCommitmentStatus}},
		{
			name: "valid learning goal",
			mutate: func(c *Commitment) {
				c.Kind, c.Deliverable, c.DueOn = CommitmentLearning, "Demo to the team", &due
			},
		},
		{
			name:    "learning goal needs a deliverable",
			mutate:  func(c *Commitment) { c.Kind, c.DueOn = CommitmentLearning, &due },
			wantErr: []error{ErrDeliverableRequired},
		},
		{
			name:    "learning goal needs a due date",
			mutate:  func(c *Commitment) { c.Kind, c.Deliverable = CommitmentLearning, "Demo" },
			wantErr: []error{ErrDueDateRequired},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := valid()
			tc.mutate(&c)
			assertErrIs(t, c.Validate(), tc.wantErr...)
		})
	}
}

func TestCommitment_IsOverdue(t *testing.T) {
	due := day(2024, 5, 1)
	tests := []struct {
		name  string
		c     Commitment
		today time.Time
		want  bool
	}{
		{name: "open and past due", c: Commitment{Status: CommitmentOpen, DueOn: &due}, today: day(2024, 5, 2), want: true},
		{name: "due today is not overdue", c: Commitment{Status: CommitmentOpen, DueOn: &due}, today: day(2024, 5, 1)},
		{name: "done is never overdue", c: Commitment{Status: CommitmentDone, DueOn: &due}, today: day(2024, 6, 1)},
		{name: "no due date", c: Commitment{Status: CommitmentOpen}, today: day(2024, 6, 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.IsOverdue(tc.today); got != tc.want {
				t.Errorf("IsOverdue() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEntry_Validate(t *testing.T) {
	plain := EntryKind{Code: "observation", Label: "Observation"}
	check := EntryKind{Code: "progress_check", Label: "Progress check", RequiresOutcome: true}
	valid := func(kind string) Entry {
		return Entry{PersonID: 1, Kind: kind, Body: "Led the incident review", OccurredAt: day(2024, 3, 1)}
	}

	tests := []struct {
		name    string
		e       Entry
		kind    EntryKind
		wantErr []error
	}{
		{name: "valid plain entry", e: valid("observation"), kind: plain},
		{name: "valid progress check", e: func() Entry { e := valid("progress_check"); e.Outcome = OutcomeProgress; return e }(), kind: check},
		{name: "blank body", e: func() Entry { e := valid("observation"); e.Body = " "; return e }(), kind: plain, wantErr: []error{ErrBodyRequired}},
		{name: "missing person", e: func() Entry { e := valid("observation"); e.PersonID = 0; return e }(), kind: plain, wantErr: []error{ErrPersonRequired}},
		{name: "missing occurred at", e: func() Entry { e := valid("observation"); e.OccurredAt = time.Time{}; return e }(), kind: plain, wantErr: []error{ErrOccurredAtRequired}},
		{name: "outcome required for the kind", e: valid("progress_check"), kind: check, wantErr: []error{ErrOutcomeRequired}},
		{name: "invalid outcome", e: func() Entry { e := valid("progress_check"); e.Outcome = "great"; return e }(), kind: check, wantErr: []error{ErrInvalidOutcome}},
		{name: "outcome not allowed for the kind", e: func() Entry { e := valid("observation"); e.Outcome = OutcomeProgress; return e }(), kind: plain, wantErr: []error{ErrOutcomeNotAllowed}},
		{name: "kind mismatch", e: valid("strength"), kind: plain, wantErr: []error{ErrKindMismatch}},
		{name: "kind required", e: valid(""), kind: plain, wantErr: []error{ErrKindRequired}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertErrIs(t, tc.e.Validate(tc.kind), tc.wantErr...)
		})
	}
}

func TestMeeting_Validate(t *testing.T) {
	oneOnOne := MeetingKind{Code: "one_on_one", Label: "1:1", RequiresPerson: true}
	team := MeetingKind{Code: "team", Label: "Team"}
	person := int64(1)

	tests := []struct {
		name    string
		m       Meeting
		kind    MeetingKind
		wantErr []error
	}{
		{name: "valid 1:1", m: Meeting{Kind: "one_on_one", PersonID: &person, StartsAt: day(2024, 3, 1)}, kind: oneOnOne},
		{name: "valid team meeting without person", m: Meeting{Kind: "team", StartsAt: day(2024, 3, 1)}, kind: team},
		{name: "1:1 needs a person", m: Meeting{Kind: "one_on_one", StartsAt: day(2024, 3, 1)}, kind: oneOnOne, wantErr: []error{ErrPersonRequired}},
		{name: "starts at required", m: Meeting{Kind: "team"}, kind: team, wantErr: []error{ErrStartsAtRequired}},
		{name: "kind required", m: Meeting{StartsAt: day(2024, 3, 1)}, kind: team, wantErr: []error{ErrKindRequired}},
		{name: "kind mismatch", m: Meeting{Kind: "standup", StartsAt: day(2024, 3, 1)}, kind: team, wantErr: []error{ErrKindMismatch}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertErrIs(t, tc.m.Validate(tc.kind), tc.wantErr...)
		})
	}
}

func TestRelease_Validate(t *testing.T) {
	tests := []struct {
		name    string
		r       Release
		wantErr []error
	}{
		{name: "valid", r: Release{Name: "R24.2", StartsOn: day(2024, 4, 1), EndsOn: day(2024, 6, 30)}},
		{name: "blank name", r: Release{StartsOn: day(2024, 4, 1), EndsOn: day(2024, 6, 30)}, wantErr: []error{ErrNameRequired}},
		{name: "ends before it starts", r: Release{Name: "R", StartsOn: day(2024, 4, 1), EndsOn: day(2024, 3, 1)}, wantErr: []error{ErrInvalidPeriod}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertErrIs(t, tc.r.Validate(), tc.wantErr...)
		})
	}
}

func TestProgressOverdue(t *testing.T) {
	now := day(2024, 6, 15)
	tests := []struct {
		name string
		last *time.Time
		want bool
	}{
		{name: "never checked", last: nil, want: true},
		{name: "14 days ago is still on time", last: ptr(day(2024, 6, 1)), want: false},
		{name: "15 days ago is overdue", last: ptr(day(2024, 5, 31)), want: true},
		{name: "yesterday", last: ptr(day(2024, 6, 14)), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProgressOverdue(tc.last, now, DefaultProgressDays); got != tc.want {
				t.Errorf("ProgressOverdue() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProgressOverdue_CustomInterval(t *testing.T) {
	now := day(2024, 6, 15)
	last := ptr(day(2024, 6, 1)) // 14 days ago
	if !ProgressOverdue(last, now, 7) {
		t.Error("14 days ago must be overdue with a 7-day interval")
	}
	if ProgressOverdue(last, now, 30) {
		t.Error("14 days ago must be on time with a 30-day interval")
	}
}

func TestConsecutiveNoProgress(t *testing.T) {
	np, p, pend := OutcomeNoProgress, OutcomeProgress, OutcomePending
	tests := []struct {
		name string
		in   []Outcome // newest first
		want int
	}{
		{name: "no checks", in: nil, want: 0},
		{name: "latest is progress", in: []Outcome{p, np, np}, want: 0},
		{name: "two in a row", in: []Outcome{np, np, p, np}, want: 2},
		{name: "pending breaks the streak", in: []Outcome{np, pend, np}, want: 1},
		{name: "all no progress", in: []Outcome{np, np, np}, want: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConsecutiveNoProgress(tc.in); got != tc.want {
				t.Errorf("ConsecutiveNoProgress() = %d, want %d", got, tc.want)
			}
		})
	}
}
