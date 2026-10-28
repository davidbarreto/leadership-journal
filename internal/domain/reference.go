package domain

import (
	"errors"
	"strings"
)

// Problems reported by Level.Validate.
var (
	ErrTrackRequired = invalid("track is required")
	ErrCodeRequired  = invalid("code is required")
	ErrInvalidRank   = invalid("rank must be positive")
)

// Validate returns every problem at once, joined.
func (l Level) Validate() error {
	var errs []error
	if l.Track == "" {
		errs = append(errs, ErrTrackRequired)
	}
	if strings.TrimSpace(l.Code) == "" {
		errs = append(errs, ErrCodeRequired)
	}
	if strings.TrimSpace(l.Title) == "" {
		errs = append(errs, ErrTitleRequired)
	}
	if l.Rank <= 0 {
		errs = append(errs, ErrInvalidRank)
	}
	return errors.Join(errs...)
}

// Prompt is one question of a meeting kind's minutes template.
type Prompt struct {
	ID          int64
	MeetingKind string
	Key         string
	Text        string
}

// CodeFromLabel derives the stable code of a user-defined kind from its label:
// lowercase ASCII letters and digits, with runs of anything else collapsed to
// one underscore. The code is immutable, so renaming the label never breaks
// references. Non-ASCII letters are dropped, hence "Café" becomes "caf".
func CodeFromLabel(label string) string {
	var b strings.Builder
	pendingSep := false
	for _, r := range strings.ToLower(label) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingSep && b.Len() > 0 {
				b.WriteByte('_')
			}
			pendingSep = false
			b.WriteRune(r)
			continue
		}
		pendingSep = true
	}
	return b.String()
}
