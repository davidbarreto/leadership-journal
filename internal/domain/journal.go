package domain

import (
	"errors"
	"strings"
	"time"
)

// CommitmentKind separates plain tasks from learning goals, which need an
// observable deliverable.
type CommitmentKind string

// The valid commitment kinds.
const (
	CommitmentTask     CommitmentKind = "task"
	CommitmentLearning CommitmentKind = "learning"
)

// Direction says who owes whom.
type Direction string

// The valid directions.
const (
	TheyOweMe Direction = "they_owe_me"
	IOweThem  Direction = "i_owe_them"
)

// CommitmentStatus is the latest status event of a commitment.
type CommitmentStatus string

// The valid commitment statuses.
const (
	CommitmentOpen    CommitmentStatus = "open"
	CommitmentDone    CommitmentStatus = "done"
	CommitmentMissed  CommitmentStatus = "missed"
	CommitmentDropped CommitmentStatus = "dropped"
)

// Valid reports whether s is one of the known statuses.
func (s CommitmentStatus) Valid() bool {
	switch s {
	case CommitmentOpen, CommitmentDone, CommitmentMissed, CommitmentDropped:
		return true
	}
	return false
}

// Problems reported by Commitment.Validate.
var (
	ErrContentRequired         = invalid("content is required")
	ErrInvalidCommitmentKind   = invalid("kind must be task or learning")
	ErrInvalidDirection        = invalid("invalid direction")
	ErrInvalidCommitmentStatus = invalid("invalid commitment status")
	ErrDeliverableRequired     = invalid("a learning goal needs a deliverable (e.g. a demo or presentation)")
	ErrDueDateRequired         = invalid("a learning goal needs a due date")
)

// Commitment is a promise, action item or learning goal tied to a person.
// Status is the latest event of its append-only status log.
type Commitment struct {
	ID          int64
	PersonID    int64
	MeetingID   *int64
	Kind        CommitmentKind
	Direction   Direction
	Content     string
	Deliverable string
	DueOn       *time.Time
	Status      CommitmentStatus
	StatusNote  string
	Lifecycle
}

// Validate returns every problem at once, joined.
func (c Commitment) Validate() error {
	var errs []error
	if strings.TrimSpace(c.Content) == "" {
		errs = append(errs, ErrContentRequired)
	}
	if c.PersonID == 0 {
		errs = append(errs, ErrPersonRequired)
	}
	switch c.Kind {
	case CommitmentTask:
	case CommitmentLearning:
		// "I read that" does not count: a learning goal must name what will be shown.
		if strings.TrimSpace(c.Deliverable) == "" {
			errs = append(errs, ErrDeliverableRequired)
		}
		if c.DueOn == nil {
			errs = append(errs, ErrDueDateRequired)
		}
	default:
		errs = append(errs, ErrInvalidCommitmentKind)
	}
	if c.Direction != TheyOweMe && c.Direction != IOweThem {
		errs = append(errs, ErrInvalidDirection)
	}
	if !c.Status.Valid() {
		errs = append(errs, ErrInvalidCommitmentStatus)
	}
	return errors.Join(errs...)
}

// IsOverdue reports whether an open commitment is past its due date.
func (c Commitment) IsOverdue(today time.Time) bool {
	return c.Status == CommitmentOpen && c.DueOn != nil && dateOnly(today).After(dateOnly(*c.DueOn))
}

// Outcome is the verdict of a progress check.
type Outcome string

// The valid outcomes.
const (
	OutcomeProgress   Outcome = "progress"
	OutcomeNoProgress Outcome = "no_progress"
	OutcomePending    Outcome = "pending"
)

// Problems reported by Entry.Validate.
var (
	ErrBodyRequired       = invalid("body is required")
	ErrOutcomeRequired    = invalid("this kind of entry needs an outcome")
	ErrInvalidOutcome     = invalid("outcome must be progress, no_progress or pending")
	ErrOutcomeNotAllowed  = invalid("this kind of entry does not take an outcome")
	ErrOccurredAtRequired = invalid("occurrence date is required")
)

// EntryKind classifies entries. Kinds are user-extensible; RequiresOutcome
// carries the one behavior the code depends on.
type EntryKind struct {
	Code            string
	Label           string
	RequiresOutcome bool
	Archived        bool
}

// Entry is the core journal item: something noted about one person,
// optionally tied to the meeting where it came up.
type Entry struct {
	ID         int64
	PersonID   int64
	MeetingID  *int64
	Kind       string
	Body       string
	Outcome    Outcome
	OccurredAt time.Time
	CorrectsID *int64
	Tags       []string
	Lifecycle
}

// Validate checks the entry against the definition of its kind.
func (e Entry) Validate(kind EntryKind) error {
	var errs []error
	if strings.TrimSpace(e.Body) == "" {
		errs = append(errs, ErrBodyRequired)
	}
	if e.PersonID == 0 {
		errs = append(errs, ErrPersonRequired)
	}
	if e.OccurredAt.IsZero() {
		errs = append(errs, ErrOccurredAtRequired)
	}
	switch {
	case e.Kind == "":
		errs = append(errs, ErrKindRequired)
	case e.Kind != kind.Code:
		errs = append(errs, ErrKindMismatch)
	}
	switch e.Outcome {
	case "":
		if kind.RequiresOutcome {
			errs = append(errs, ErrOutcomeRequired)
		}
	case OutcomeProgress, OutcomeNoProgress, OutcomePending:
		if !kind.RequiresOutcome {
			errs = append(errs, ErrOutcomeNotAllowed)
		}
	default:
		errs = append(errs, ErrInvalidOutcome)
	}
	return errors.Join(errs...)
}

// ErrStartsAtRequired is returned for a meeting without a start time.
var ErrStartsAtRequired = invalid("start time is required")

// MeetingKind classifies meetings. RequiresPerson is the behavior flag: a 1:1
// is always about one person, a team meeting is not.
type MeetingKind struct {
	Code           string
	Label          string
	RequiresPerson bool
	Archived       bool
}

// Answer is the response to one prompt of a meeting's minutes template. The
// prompt wording is snapshotted so rewording a prompt later does not change
// what past meetings show.
type Answer struct {
	PromptKey  string
	PromptText string
	Text       string
}

// Meeting is a 1:1, team meeting, standup or other. StartsAt keeps the
// time zone the meeting happened in.
type Meeting struct {
	ID       int64
	Kind     string
	Title    string
	PersonID *int64
	TeamID   *int64
	StartsAt time.Time
	Notes    string
	Answers  []Answer
	Lifecycle
}

// Validate checks the meeting against the definition of its kind.
func (m Meeting) Validate(kind MeetingKind) error {
	var errs []error
	switch {
	case m.Kind == "":
		errs = append(errs, ErrKindRequired)
	case m.Kind != kind.Code:
		errs = append(errs, ErrKindMismatch)
	}
	if kind.RequiresPerson && m.PersonID == nil {
		errs = append(errs, ErrPersonRequired)
	}
	if m.StartsAt.IsZero() {
		errs = append(errs, ErrStartsAtRequired)
	}
	return errors.Join(errs...)
}

// Team groups the people a lead manages.
type Team struct {
	ID   int64
	Name string
}

// Release is the unit of the "one stretch task per release" rule.
type Release struct {
	ID       int64
	Name     string
	StartsOn time.Time
	EndsOn   time.Time
}

// Validate returns every problem at once, joined.
func (r Release) Validate() error {
	var errs []error
	if strings.TrimSpace(r.Name) == "" {
		errs = append(errs, ErrNameRequired)
	}
	if r.EndsOn.Before(r.StartsOn) {
		errs = append(errs, ErrInvalidPeriod)
	}
	return errors.Join(errs...)
}

// progressInterval is how often the improvement journal is updated.
const progressInterval = 14

// ProgressOverdue reports whether a person's last finalized progress check is
// older than two weeks (or does not exist).
func ProgressOverdue(last *time.Time, now time.Time) bool {
	if last == nil {
		return true
	}
	days := int(dateOnly(now).Sub(dateOnly(*last)).Hours() / 24)
	return days > progressInterval
}

// ConsecutiveNoProgress counts the leading no_progress outcomes of a list
// ordered newest first. Any other outcome ends the streak.
func ConsecutiveNoProgress(newestFirst []Outcome) int {
	n := 0
	for _, o := range newestFirst {
		if o != OutcomeNoProgress {
			break
		}
		n++
	}
	return n
}
