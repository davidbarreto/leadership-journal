package store

import (
	"database/sql"
	"fmt"
	"time"
)

const dateLayout = "2006-01-02"

// formatStamp splits an instant into its UTC text and the zone it was in.
func formatStamp(t time.Time) (at, tz string) {
	return t.UTC().Format(time.RFC3339), t.Location().String()
}

// parseStamp rebuilds an instant in the zone it was recorded in. An unknown
// zone name falls back to UTC rather than failing a whole page.
func parseStamp(at, tz string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", at, err)
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return t.In(loc), nil
}

// nullStamp is formatStamp for optional instants.
func nullStamp(t *time.Time) (at, tz sql.NullString) {
	if t == nil {
		return
	}
	a, z := formatStamp(*t)
	return sql.NullString{String: a, Valid: true}, sql.NullString{String: z, Valid: true}
}

// scanNullStamp is parseStamp for optional instants.
func scanNullStamp(at, tz sql.NullString) (*time.Time, error) {
	if !at.Valid {
		return nil, nil
	}
	t, err := parseStamp(at.String, tz.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// formatDate keeps the calendar date as written, ignoring the time of day.
func formatDate(t time.Time) string { return t.Format(dateLayout) }

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse date %q: %w", s, err)
	}
	return t, nil
}

func nullDate(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: formatDate(*t), Valid: true}
}

func scanNullDate(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := parseDate(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
