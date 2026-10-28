package domain

import (
	"errors"
	"time"
)

// Problems reported by LevelHistory.
var (
	ErrLevelAlreadyHeld  = errors.New("person already holds this level")
	ErrRankNotIncreasing = errors.New("within a track, a later date must have a higher rank")
	ErrNoLevel           = errors.New("person had no level on that date")
)

// LevelAssignment records that a person holds Level from EffectiveFrom on.
type LevelAssignment struct {
	Level         Level
	EffectiveFrom time.Time
}

// LevelHistory is the sequence of levels a person has held, so a review can
// refer to the level held at the time.
type LevelHistory []LevelAssignment

// Add appends an assignment if it keeps the history consistent: each level is
// held once, and within a track the rank grows with the date. Assignments of
// other tracks are ignored by the rank rule because their ranks are unrelated.
// On error the history is left unchanged.
func (h *LevelHistory) Add(a LevelAssignment) error {
	from := dateOnly(a.EffectiveFrom)

	for _, held := range *h {
		if held.Level.ID == a.Level.ID {
			return ErrLevelAlreadyHeld
		}
		if held.Level.Track != a.Level.Track {
			continue
		}
		heldFrom := dateOnly(held.EffectiveFrom)
		// Equal dates are allowed: nothing says which of two same-day
		// levels came first, and LevelAt resolves it by rank.
		if (heldFrom.Before(from) && held.Level.Rank >= a.Level.Rank) ||
			(heldFrom.After(from) && held.Level.Rank <= a.Level.Rank) {
			return ErrRankNotIncreasing
		}
	}

	*h = append(*h, a)
	return nil
}

// LevelAt returns the level in force on date, or ErrNoLevel if the person had
// none yet. If two levels of the same track share the latest effective date,
// the higher rank wins; across tracks, the one added last wins.
func (h LevelHistory) LevelAt(date time.Time) (Level, error) {
	date = dateOnly(date)

	var best LevelAssignment
	found := false
	for _, a := range h {
		from := dateOnly(a.EffectiveFrom)
		if from.After(date) {
			continue
		}
		if !found || supersedes(a, best) {
			best, found = a, true
		}
	}

	if !found {
		return Level{}, ErrNoLevel
	}
	return best.Level, nil
}

func supersedes(a, than LevelAssignment) bool {
	af, tf := dateOnly(a.EffectiveFrom), dateOnly(than.EffectiveFrom)
	if !af.Equal(tf) {
		return af.After(tf)
	}
	return a.Level.Track != than.Level.Track || a.Level.Rank > than.Level.Rank
}

// dateOnly keeps the calendar date as written and drops the time of day, so
// "10 Jan 23:59" is the same day as "10 Jan 00:00" whatever the time zone.
func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
