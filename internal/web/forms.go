package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"leadership-journal/internal/store"
)

func formStr(r *http.Request, key string) string { return strings.TrimSpace(r.PostFormValue(key)) }

// formID reads an id field; empty means 0 (not chosen).
func formID(r *http.Request, key string) (int64, error) {
	v := formStr(r, key)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0, userErrorf("Invalid selection")
	}
	return n, nil
}

// formIDPtr is formID for optional references: nil when nothing was chosen.
func formIDPtr(r *http.Request, key string) (*int64, error) {
	n, err := formID(r, key)
	if err != nil || n == 0 {
		return nil, err
	}
	return &n, nil
}

// Layouts of the HTML date and datetime-local inputs. Browsers may add seconds
// to the latter.
const (
	dateLayout          = "2006-01-02"
	dateTimeLayout      = "2006-01-02T15:04"
	dateTimeLayoutShort = "2006-01-02T15:04:05"
)

// formDate reads a date input as midnight in the owner's time zone; empty
// gives the zero time.
func (s *Server) formDate(r *http.Request, key string) (time.Time, error) {
	v := formStr(r, key)
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.ParseInLocation(dateLayout, v, s.loc)
	if err != nil {
		return time.Time{}, userErrorf("Invalid date: use the form YYYY-MM-DD")
	}
	return t, nil
}

func (s *Server) formDatePtr(r *http.Request, key string) (*time.Time, error) {
	t, err := s.formDate(r, key)
	if err != nil || t.IsZero() {
		return nil, err
	}
	return &t, nil
}

// formDateTime reads a datetime-local input in the owner's time zone.
func (s *Server) formDateTime(r *http.Request, key string) (time.Time, error) {
	v := formStr(r, key)
	if v == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{dateTimeLayout, dateTimeLayoutShort} {
		if t, err := time.ParseInLocation(layout, v, s.loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, userErrorf("Invalid date and time")
}

// occurredAt turns an optional date input into the moment an entry happened:
// midday of that day, so entries of one day sort together without pretending
// to a precise time.
func (s *Server) occurredAt(r *http.Request, key string, fallback time.Time) (time.Time, error) {
	d, err := s.formDate(r, key)
	if err != nil {
		return time.Time{}, err
	}
	if d.IsZero() {
		return fallback, nil
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, s.loc), nil
}

// pathID reads the {id} path value; anything that is not a positive number is
// simply not found.
func pathID(r *http.Request) (int64, error) {
	n, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || n <= 0 {
		return 0, store.ErrNotFound
	}
	return n, nil
}

// parseTags splits a comma-separated tag field.
func parseTags(raw string) []string {
	var tags []string
	for _, t := range strings.Split(raw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

func csrfOf(r *http.Request) string {
	sess, _ := sessionFromContext(r.Context())
	return sess.CSRFToken
}

// done confirms an action with a one-time message and returns to where the
// user came from.
func (s *Server) done(w http.ResponseWriter, r *http.Request, fallback, message string) error {
	s.setFlash(w, flashOK, message)
	http.Redirect(w, r, s.backURL(r, fallback), http.StatusSeeOther)
	return nil
}

// redirect goes to a fixed page, ignoring any "back" field.
func (s *Server) redirect(w http.ResponseWriter, r *http.Request, to, message string) error {
	if message != "" {
		s.setFlash(w, flashOK, message)
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
	return nil
}
