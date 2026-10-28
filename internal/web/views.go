package web

import (
	"context"
	"net/http"
	"time"

	"leadership-journal/internal/domain"
)

// entryView is one entry as a template partial needs it. The partial posts
// forms, so it carries the CSRF token and the page to return to.
type entryView struct {
	Entry     domain.Entry
	KindLabel string
	Back      string
	CSRF      string
}

type commitmentView struct {
	C          domain.Commitment
	PersonName string
	Overdue    bool
	Back       string
	CSRF       string
}

// entryFormData and commitmentFormData feed the "add" form partials. When
// PersonID is 0 the form asks which person it is about.
type entryFormData struct {
	PersonID  int64
	MeetingID int64
	People    []domain.Person
	Kinds     []domain.EntryKind
	Tags      []string
	Today     string
	Back      string
	CSRF      string
}

type commitmentFormData struct {
	PersonID  int64
	MeetingID int64
	People    []domain.Person
	Back      string
	CSRF      string
}

func (s *Server) entryKindLabels(ctx context.Context) (kinds []domain.EntryKind, labels map[string]string, err error) {
	kinds, err = s.store.ListEntryKinds(ctx)
	if err != nil {
		return nil, nil, err
	}
	labels = make(map[string]string, len(kinds))
	for _, k := range kinds {
		labels[k.Code] = k.Label
	}
	return kinds, labels, nil
}

func label(labels map[string]string, code string) string {
	if l, ok := labels[code]; ok {
		return l
	}
	return human(code)
}

func (s *Server) entryViews(entries []domain.Entry, labels map[string]string, back string, r *http.Request) []entryView {
	out := make([]entryView, len(entries))
	for i, e := range entries {
		out[i] = entryView{Entry: e, KindLabel: label(labels, e.Kind), Back: back, CSRF: csrfOf(r)}
	}
	return out
}

func (s *Server) commitmentViews(list []domain.Commitment, names map[int64]string, back string, r *http.Request) []commitmentView {
	today := s.now()
	out := make([]commitmentView, len(list))
	for i, c := range list {
		out[i] = commitmentView{C: c, PersonName: names[c.PersonID], Overdue: c.IsOverdue(today), Back: back, CSRF: csrfOf(r)}
	}
	return out
}

func (s *Server) personNames(ctx context.Context) (map[int64]string, error) {
	people, err := s.store.ListPeople(ctx, "")
	if err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(people))
	for _, p := range people {
		names[p.ID] = p.Name
	}
	return names, nil
}

// currentLevelCode is the code of the level a person holds today, or "".
func (s *Server) currentLevelCode(ctx context.Context, personID int64, at time.Time) (string, error) {
	history, err := s.store.LevelHistory(ctx, personID)
	if err != nil {
		return "", err
	}
	level, err := history.LevelAt(at)
	if err != nil {
		return "", nil // no level recorded yet
	}
	return level.Code, nil
}
