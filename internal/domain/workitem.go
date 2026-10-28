package domain

import (
	"errors"
	"strings"
	"time"
)

// Stretch is the lead's judgment of how far a task stretches one specific
// person. It is deliberately not derived from levels: difficulty depends on
// the person's experience and business knowledge.
type Stretch string

// The valid stretch judgments.
const (
	StretchBelow Stretch = "below"
	StretchAt    Stretch = "at"
	StretchAbove Stretch = "above"
)

// PickedBy says who chose a task: the person themselves or the lead.
type PickedBy string

// The valid values of PickedBy.
const (
	PickedBySelf PickedBy = "self"
	PickedByLead PickedBy = "lead"
)

// WorkStatus is the progress of a work item. Moved means the task was
// reassigned and a new work item exists for the new person.
type WorkStatus string

// The valid work statuses.
const (
	WorkPlanned    WorkStatus = "planned"
	WorkInProgress WorkStatus = "in_progress"
	WorkDone       WorkStatus = "done"
	WorkDropped    WorkStatus = "dropped"
	WorkMoved      WorkStatus = "moved"
)

// Problems reported by WorkItem.Validate.
var (
	ErrTitleRequired      = errors.New("title is required")
	ErrReleaseRequired    = errors.New("release is required")
	ErrInvalidPickedBy    = errors.New("picked by must be self or lead")
	ErrInvalidStretch     = errors.New("stretch must be below, at or above")
	ErrInvalidWorkStatus  = errors.New("invalid work status")
	ErrRationaleRequired  = errors.New("stretch rationale is required")
	ErrAssignedAtRequired = errors.New("assignment date is required")
)

// WorkItem links one task to one person within a release. A ticket is never
// reused by someone else, so reassigning creates a new WorkItem.
type WorkItem struct {
	ID                int64
	PersonID          int64
	ReleaseID         int64
	Title             string
	ExternalRef       string
	PickedBy          PickedBy
	Stretch           Stretch
	StretchRationale  string
	Status            WorkStatus
	InvestigationDone bool
	InvestigationURL  string
	OutcomeNotes      string
	AssignedAt        time.Time
	CompletedAt       *time.Time
	MovedFromID       *int64
	Lifecycle
}

// Validate returns every problem at once, joined.
func (w WorkItem) Validate() error {
	var errs []error
	if strings.TrimSpace(w.Title) == "" {
		errs = append(errs, ErrTitleRequired)
	}
	if w.PersonID == 0 {
		errs = append(errs, ErrPersonRequired)
	}
	if w.ReleaseID == 0 {
		errs = append(errs, ErrReleaseRequired)
	}
	if w.PickedBy != PickedBySelf && w.PickedBy != PickedByLead {
		errs = append(errs, ErrInvalidPickedBy)
	}
	switch w.Stretch {
	case StretchBelow, StretchAt, StretchAbove:
	default:
		errs = append(errs, ErrInvalidStretch)
	}
	switch w.Status {
	case WorkPlanned, WorkInProgress, WorkDone, WorkDropped, WorkMoved:
	default:
		errs = append(errs, ErrInvalidWorkStatus)
	}
	if strings.TrimSpace(w.StretchRationale) == "" {
		errs = append(errs, ErrRationaleRequired)
	}
	if w.AssignedAt.IsZero() {
		errs = append(errs, ErrAssignedAtRequired)
	}
	return errors.Join(errs...)
}

// counts reports whether the item is evidence of work by its person: only
// finalized rows count (drafts are not yet part of the record), and tasks that
// were dropped or handed to someone else are not that person's work.
func (w WorkItem) counts() bool {
	return w.IsFinalized() && !w.IsVoided() && w.Status != WorkDropped && w.Status != WorkMoved
}

// ReleaseCompliant implements the process rule "at least one task above the
// person's level per release", given one person's items in one release.
func ReleaseCompliant(items []WorkItem) bool {
	for _, w := range items {
		if w.counts() && w.Stretch == StretchAbove {
			return true
		}
	}
	return false
}

// WorkMetrics summarizes the tasks that count towards a person's record.
type WorkMetrics struct {
	Total             int
	Above             int
	AboveSelf         int
	AboveLead         int
	InvestigationDone int
}

// ComputeWorkMetrics tallies the items that count (see WorkItem.counts).
func ComputeWorkMetrics(items []WorkItem) WorkMetrics {
	var m WorkMetrics
	for _, w := range items {
		if !w.counts() {
			continue
		}
		m.Total++
		if w.InvestigationDone {
			m.InvestigationDone++
		}
		if w.Stretch != StretchAbove {
			continue
		}
		m.Above++
		if w.PickedBy == PickedBySelf {
			m.AboveSelf++
		} else {
			m.AboveLead++
		}
	}
	return m
}

// PctAboveSelf is the share of above-stretch tasks the person picked themselves.
func (m WorkMetrics) PctAboveSelf() float64 { return pct(m.AboveSelf, m.Above) }

// PctAboveLead is the share of above-stretch tasks the lead assigned.
func (m WorkMetrics) PctAboveLead() float64 { return pct(m.AboveLead, m.Above) }

// PctInvestigated is the share of tasks that had an investigation first.
func (m WorkMetrics) PctInvestigated() float64 { return pct(m.InvestigationDone, m.Total) }

func pct(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) * 100 / float64(whole)
}
