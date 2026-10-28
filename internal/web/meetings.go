package web

import (
	"context"
	"fmt"
	"net/http"

	"leadership-journal/internal/domain"
	"leadership-journal/internal/store"
)

func (s *Server) registerMeetings(rt router) {
	rt.protected("GET /meetings", s.meetingsList)
	rt.protected("POST /meetings", s.meetingCreate)
	rt.protected("GET /meetings/{id}", s.meetingShow)
	rt.protected("POST /meetings/{id}", s.meetingSave)
	rt.protected("POST /meetings/{id}/finalize", s.meetingFinalize)
	rt.protected("POST /meetings/{id}/void", s.meetingVoid)
	rt.protected("POST /meetings/{id}/delete", s.meetingDelete)
}

type meetingRow struct {
	Meeting    domain.Meeting
	PersonName string
	KindLabel  string
}

func (s *Server) meetingRows(ctx context.Context, meetings []domain.Meeting) ([]meetingRow, error) {
	names, err := s.personNames(ctx)
	if err != nil {
		return nil, err
	}
	kinds, err := s.store.ListMeetingKinds(ctx)
	if err != nil {
		return nil, err
	}
	labels := map[string]string{}
	for _, k := range kinds {
		labels[k.Code] = k.Label
	}
	rows := make([]meetingRow, len(meetings))
	for i, m := range meetings {
		rows[i] = meetingRow{Meeting: m, KindLabel: label(labels, m.Kind)}
		if m.PersonID != nil {
			rows[i].PersonName = names[*m.PersonID]
		}
	}
	return rows, nil
}

// meetingPeople are the people a meeting can be about: everyone but the owner.
func (s *Server) meetingPeople(ctx context.Context) ([]domain.Person, error) {
	people, err := s.store.ListPeople(ctx, "")
	if err != nil {
		return nil, err
	}
	var out []domain.Person
	for _, p := range people {
		if p.Relationship != domain.RelationshipSelf {
			out = append(out, p)
		}
	}
	return out, nil
}

type meetingsPage struct {
	Rows     []meetingRow
	Kinds    []domain.MeetingKind
	People   []domain.Person
	StartsAt string
}

func (s *Server) meetingsList(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	meetings, err := s.store.ListMeetings(ctx, store.MeetingFilter{Limit: 200})
	if err != nil {
		return err
	}
	rows, err := s.meetingRows(ctx, meetings)
	if err != nil {
		return err
	}
	kinds, err := s.store.ListMeetingKinds(ctx)
	if err != nil {
		return err
	}
	people, err := s.meetingPeople(ctx)
	if err != nil {
		return err
	}
	return s.render(w, r, http.StatusOK, "meetings", "meetings", meetingsPage{
		Rows: rows, Kinds: kinds, People: people, StartsAt: s.now().Format(dateTimeLayout),
	})
}

func (s *Server) meetingCreate(w http.ResponseWriter, r *http.Request) error {
	personID, err := formIDPtr(r, "person_id")
	if err != nil {
		return err
	}
	teamID, err := formIDPtr(r, "team_id")
	if err != nil {
		return err
	}
	startsAt, err := s.formDateTime(r, "starts_at")
	if err != nil {
		return err
	}
	if startsAt.IsZero() {
		startsAt = s.now()
	}
	id, err := s.store.CreateMeeting(r.Context(), domain.Meeting{
		Kind: formStr(r, "kind"), Title: formStr(r, "title"), PersonID: personID, TeamID: teamID, StartsAt: startsAt,
	})
	if err != nil {
		return err
	}
	s.log.Info("meeting created", "meeting_id", id, "kind", formStr(r, "kind"), "request_id", requestID(r.Context()))
	return s.redirect(w, r, fmt.Sprintf("/meetings/%d", id), "Meeting started as a draft.")
}

type promptField struct {
	Key, Text, Value string
}

type meetingPage struct {
	Meeting     domain.Meeting
	KindLabel   string
	PersonName  string
	Editable    bool
	Fields      []promptField
	Answers     []domain.Answer
	Entries     []entryView
	Commitments []commitmentView
	People      []domain.Person
	StartsAt    string
	EntryForm   entryFormData
	CommitForm  commitmentFormData
	Back        string
	CSRF        string
}

func (s *Server) meetingShow(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		return err
	}
	m, err := s.store.GetMeeting(ctx, id)
	if err != nil {
		return err
	}
	kind, err := s.store.GetMeetingKind(ctx, m.Kind)
	if err != nil {
		return err
	}
	people, err := s.meetingPeople(ctx)
	if err != nil {
		return err
	}
	names, err := s.personNames(ctx)
	if err != nil {
		return err
	}

	back := fmt.Sprintf("/meetings/%d", id)
	page := meetingPage{
		Meeting: m, KindLabel: kind.Label, Editable: m.IsDraft(), Answers: m.Answers, People: people,
		StartsAt: m.StartsAt.Format(dateTimeLayout), Back: back, CSRF: csrfOf(r),
	}
	if m.PersonID != nil {
		page.PersonName = names[*m.PersonID]
	}
	if page.Editable {
		if page.Fields, err = s.promptFields(ctx, m); err != nil {
			return err
		}
	}

	kinds, labels, err := s.entryKindLabels(ctx)
	if err != nil {
		return err
	}
	entries, err := s.store.ListEntries(ctx, store.EntryFilter{MeetingID: &id, IncludeVoided: true})
	if err != nil {
		return err
	}
	page.Entries = s.entryViews(entries, labels, back, r)
	commitments, err := s.store.ListCommitments(ctx, store.CommitmentFilter{MeetingID: &id, IncludeVoided: true})
	if err != nil {
		return err
	}
	page.Commitments = s.commitmentViews(commitments, names, back, r)

	tags, err := s.store.ListTags(ctx)
	if err != nil {
		return err
	}
	var personID int64
	if m.PersonID != nil {
		personID = *m.PersonID
	}
	page.EntryForm = entryFormData{PersonID: personID, MeetingID: id, People: people, Kinds: kinds, Tags: tags, Back: back, CSRF: csrfOf(r)}
	page.CommitForm = commitmentFormData{PersonID: personID, MeetingID: id, People: people, Back: back, CSRF: csrfOf(r)}
	return s.render(w, r, http.StatusOK, "meeting", "meetings", page)
}

// promptFields lists the questions to answer: the kind's current prompts, then
// answers to prompts that were archived since, so saving never drops them.
func (s *Server) promptFields(ctx context.Context, m domain.Meeting) ([]promptField, error) {
	prompts, err := s.store.ListPrompts(ctx, m.Kind)
	if err != nil {
		return nil, err
	}
	existing := map[string]domain.Answer{}
	for _, a := range m.Answers {
		existing[a.PromptKey] = a
	}
	var fields []promptField
	seen := map[string]bool{}
	for _, p := range prompts {
		fields = append(fields, promptField{Key: p.Key, Text: p.Text, Value: existing[p.Key].Text})
		seen[p.Key] = true
	}
	for _, a := range m.Answers {
		if !seen[a.PromptKey] {
			fields = append(fields, promptField{Key: a.PromptKey, Text: a.PromptText, Value: a.Text})
		}
	}
	return fields, nil
}

func (s *Server) meetingSave(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		return err
	}
	m, err := s.store.GetMeeting(ctx, id)
	if err != nil {
		return err
	}
	fields, err := s.promptFields(ctx, m)
	if err != nil {
		return err
	}
	startsAt, err := s.formDateTime(r, "starts_at")
	if err != nil {
		return err
	}
	if !startsAt.IsZero() {
		m.StartsAt = startsAt
	}
	if m.PersonID, err = formIDPtr(r, "person_id"); err != nil {
		return err
	}
	m.Title = formStr(r, "title")
	m.Notes = r.PostFormValue("notes")
	m.Answers = nil
	for _, f := range fields {
		m.Answers = append(m.Answers, domain.Answer{PromptKey: f.Key, PromptText: f.Text, Text: r.PostFormValue("answer_" + f.Key)})
	}
	if err := s.store.UpdateMeeting(ctx, m); err != nil {
		return err
	}
	return s.done(w, r, fmt.Sprintf("/meetings/%d", id), "Draft saved.")
}

func (s *Server) meetingFinalize(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.store.FinalizeMeeting(r.Context(), id); err != nil {
		return err
	}
	s.log.Info("meeting finalized", "meeting_id", id, "request_id", requestID(r.Context()))
	return s.redirect(w, r, fmt.Sprintf("/meetings/%d", id), "Meeting finalized. It is now part of the record.")
}

func (s *Server) meetingVoid(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.store.VoidMeeting(r.Context(), id, formStr(r, "reason")); err != nil {
		return err
	}
	s.log.Info("meeting voided", "meeting_id", id, "request_id", requestID(r.Context()))
	return s.redirect(w, r, fmt.Sprintf("/meetings/%d", id), "Meeting voided. It stays on record, marked as wrong.")
}

func (s *Server) meetingDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.store.DeleteDraftMeeting(r.Context(), id); err != nil {
		return err
	}
	s.log.Info("draft meeting deleted", "meeting_id", id, "request_id", requestID(r.Context()))
	return s.redirect(w, r, "/meetings", "Draft deleted.")
}
