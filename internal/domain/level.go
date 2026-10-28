// Package domain holds the journal's core types and business rules.
// It stays free of database, HTTP and logging code so it is trivial to test.
package domain

// Track identifies a seniority scale; levels are only comparable within one.
type Track string

// The known tracks.
const (
	TrackEngineer Track = "engineer"
	TrackManager  Track = "manager"
	TrackQA       Track = "qa"
)

// ErrDifferentTrack is returned when comparing levels of different tracks.
var ErrDifferentTrack = invalid("levels belong to different tracks")

// Level is one step of a seniority scale, e.g. E4 or EM2.
type Level struct {
	ID    int64
	Track Track
	Code  string
	Title string
	// Rank orders levels within a Track only. Values are seeded with gaps
	// (10, 20, ...) so a level can be inserted between two others without
	// renumbering.
	Rank int
}

// IsAbove reports whether l is more senior than other. Ranks of different
// tracks are unrelated (E6 vs EM1), so that is an error rather than a guess.
func (l Level) IsAbove(other Level) (bool, error) {
	if l.Track != other.Track {
		return false, ErrDifferentTrack
	}
	return l.Rank > other.Rank, nil
}
