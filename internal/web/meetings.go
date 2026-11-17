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

// newNoteFields and newCommitmentFields hold what was typed in the meeting
// page's "new note" and "new commitment" parts, so a refused save can show
// it again.
type newNoteFields struct {
	PersonID, Kind, Outcome, Body, Tags string
}

type newCommitmentFields struct {
	PersonID, Kind, Direction, DueOn, Content, Deliverable string
}

type meetingPage struct {
	Meeting       domain.Meeting
	KindLabel     string
	PersonName    string
	Editable      bool
	Fields        []promptField
	Answers       []domain.Answer
	Entries       []entryView
	Commitments   []commitmentView
	People        []domain.Person
	StartsAt      string
	EntryKinds    []domain.EntryKind
	Tags          []string
	NewNote       newNoteFields
	NewCommitment newCommitmentFields
	Back          string
	CSRF          string
}

func (s *Server) meetingShow(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	m, err := s.store.GetMeeting(r.Context(), id)
	if err != nil {
		return err
	}
	page, err := s.meetingPageFor(r, m)
	if err != nil {
		return err
	}
	return s.render(w, r, http.StatusOK, "meeting", "meetings", page)
}

// meetingPageFor builds the page of meeting m, which may carry edits that
// were not saved.
func (s *Server) meetingPageFor(r *http.Request, m domain.Meeting) (meetingPage, error) {
	ctx := r.Context()
	kind, err := s.store.GetMeetingKind(ctx, m.Kind)
	if err != nil {
		return meetingPage{}, err
	}
	people, err := s.meetingPeople(ctx)
	if err != nil {
		return meetingPage{}, err
	}
	names, err := s.personNames(ctx)
	if err != nil {
		return meetingPage{}, err
	}

	back := fmt.Sprintf("/meetings/%d", m.ID)
	page := meetingPage{
		Meeting: m, KindLabel: kind.Label, Editable: m.IsDraft(), Answers: m.Answers, People: people,
		StartsAt: m.StartsAt.Format(dateTimeLayout), Back: back, CSRF: csrfOf(r),
		NewCommitment: newCommitmentFields{Kind: string(domain.CommitmentTask), Direction: string(domain.TheyOweMe)},
	}
	if m.PersonID != nil {
		page.PersonName = names[*m.PersonID]
	}
	if page.Editable {
		if page.Fields, err = s.promptFields(ctx, m); err != nil {
			return meetingPage{}, err
		}
		if page.Tags, err = s.store.ListTags(ctx); err != nil {
			return meetingPage{}, err
		}
	}

	kinds, labels, err := s.entryKindLabels(ctx)
	if err != nil {
		return meetingPage{}, err
	}
	page.EntryKinds = kinds
	entries, err := s.store.ListEntries(ctx, store.EntryFilter{MeetingID: &m.ID, IncludeVoided: true})
	if err != nil {
		return meetingPage{}, err
	}
	page.Entries = s.entryViews(entries, labels, back, r)
	commitments, err := s.store.ListCommitments(ctx, store.CommitmentFilter{MeetingID: &m.ID, IncludeVoided: true})
	if err != nil {
		return meetingPage{}, err
	}
	page.Commitments = s.commitmentViews(commitments, names, back, r)
	return page, nil
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

// meetingSave saves everything typed on a draft meeting's page in one go:
// the minutes, plus a new note and a new commitment when those were filled
// in, and finalizes it all when asked to.
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
	save, err := s.meetingSaveFromForm(r, m)
	if err == nil {
		err = s.store.SaveMeeting(ctx, save)
	}
	// A refused draft is shown again with what was typed, instead of the
	// usual redirect, which would lose it.
	if isUserError(err) && m.IsDraft() {
		page, perr := s.meetingPageFor(r, save.Meeting)
		if perr != nil {
			return perr
		}
		page.NewNote = newNoteFields{
			PersonID: formStr(r, "note_person_id"), Kind: formStr(r, "note_kind"), Outcome: formStr(r, "note_outcome"),
			Body: r.PostFormValue("note_body"), Tags: formStr(r, "note_tags"),
		}
		page.NewCommitment = newCommitmentFields{
			PersonID: formStr(r, "c_person_id"), Kind: formStr(r, "c_kind"), Direction: formStr(r, "c_direction"),
			DueOn: formStr(r, "c_due_on"), Content: formStr(r, "c_content"), Deliverable: formStr(r, "c_deliverable"),
		}
		s.log.Info("meeting save refused", "meeting_id", id, "request_id", requestID(ctx))
		return s.renderFlash(w, r, http.StatusUnprocessableEntity, "meeting", "meetings", page,
			&flash{Kind: flashError, Message: userMessage(err) + " Nothing was saved yet."})
	}
	if err != nil {
		return err
	}
	s.log.Info("meeting saved", "meeting_id", id, "new_entry", save.NewEntry != nil,
		"new_commitment", save.NewCommitment != nil, "finalized", save.Finalize, "request_id", requestID(ctx))
	if save.Finalize {
		return s.redirect(w, r, fmt.Sprintf("/meetings/%d", id), "Meeting finalized. It is now part of the record.")
	}
	return s.done(w, r, fmt.Sprintf("/meetings/%d", id), "Draft saved.")
}

// meetingSaveFromForm reads the meeting page. The returned save always holds
// the edited meeting, even with an error, so the page can be shown again.
func (s *Server) meetingSaveFromForm(r *http.Request, m domain.Meeting) (store.MeetingSave, error) {
	fields, err := s.promptFields(r.Context(), m)
	if err != nil {
		return store.MeetingSave{Meeting: m}, err
	}
	m.Title = formStr(r, "title")
	m.Notes = r.PostFormValue("notes")
	m.Answers = nil
	for _, f := range fields {
		m.Answers = append(m.Answers, domain.Answer{PromptKey: f.Key, PromptText: f.Text, Text: r.PostFormValue("answer_" + f.Key)})
	}
	save := store.MeetingSave{Meeting: m, Finalize: formStr(r, "then") == "finalize"}

	startsAt, err := s.formDateTime(r, "starts_at")
	if err != nil {
		return save, err
	}
	if !startsAt.IsZero() {
		save.Meeting.StartsAt = startsAt
	}
	if save.Meeting.PersonID, err = formIDPtr(r, "person_id"); err != nil {
		return save, err
	}

	if save.NewEntry, err = newMeetingEntry(r, save.Meeting); err != nil {
		return save, err
	}
	save.NewCommitment, err = s.newMeetingCommitment(r, save.Meeting)
	return save, err
}

// meetingSubject is who a new note or commitment is about: the one chosen
// in its own field (offered when the meeting is not about one person), or
// else the meeting's person.
func meetingSubject(r *http.Request, field string, m domain.Meeting) (int64, error) {
	id, err := formID(r, field)
	if err != nil || id != 0 || m.PersonID == nil {
		return id, err
	}
	return *m.PersonID, nil
}

// newMeetingEntry is the note typed on the meeting page, or nil when its
// fields were left empty.
func newMeetingEntry(r *http.Request, m domain.Meeting) (*domain.Entry, error) {
	if formStr(r, "note_body") == "" && formStr(r, "note_tags") == "" && formStr(r, "note_outcome") == "" {
		return nil, nil
	}
	personID, err := meetingSubject(r, "note_person_id", m)
	if err != nil {
		return nil, err
	}
	return &domain.Entry{
		PersonID: personID, Kind: formStr(r, "note_kind"), Body: formStr(r, "note_body"),
		Outcome: domain.Outcome(formStr(r, "note_outcome")), Tags: parseTags(r.PostFormValue("note_tags")),
	}, nil
}

// newMeetingCommitment is the commitment typed on the meeting page, or nil
// when its fields were left empty.
func (s *Server) newMeetingCommitment(r *http.Request, m domain.Meeting) (*domain.Commitment, error) {
	if formStr(r, "c_content") == "" && formStr(r, "c_deliverable") == "" && formStr(r, "c_due_on") == "" {
		return nil, nil
	}
	personID, err := meetingSubject(r, "c_person_id", m)
	if err != nil {
		return nil, err
	}
	due, err := s.formDatePtr(r, "c_due_on")
	if err != nil {
		return nil, err
	}
	return &domain.Commitment{
		PersonID: personID, Kind: domain.CommitmentKind(formStr(r, "c_kind")), Direction: domain.Direction(formStr(r, "c_direction")),
		Content: formStr(r, "c_content"), Deliverable: formStr(r, "c_deliverable"), DueOn: due,
	}, nil
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
