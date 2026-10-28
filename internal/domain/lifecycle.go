package domain

import (
	"time"
)

// Problems shared by several journal types.
var (
	ErrPersonRequired = invalid("person is required")
	ErrKindRequired   = invalid("kind is required")
	ErrKindMismatch   = invalid("kind does not match its definition")
	ErrInvalidPeriod  = invalid("end date is before start date")
)

// Lifecycle tracks where a journal row is in the draft, finalized, voided
// progression. Finalized rows are locked by the database; voided rows are
// hidden but never removed.
type Lifecycle struct {
	CreatedAt   time.Time
	FinalizedAt *time.Time
	VoidedAt    *time.Time
	VoidReason  string
}

// IsDraft reports whether the row can still be freely edited and deleted.
func (l Lifecycle) IsDraft() bool { return l.FinalizedAt == nil && l.VoidedAt == nil }

// IsFinalized reports whether the row has been locked (voided rows stay finalized).
func (l Lifecycle) IsFinalized() bool { return l.FinalizedAt != nil }

// IsVoided reports whether the row was declared wrong after being locked.
func (l Lifecycle) IsVoided() bool { return l.VoidedAt != nil }
