package web

import (
	"fmt"
	"net/http"
	"strings"

	"leadership-journal/internal/domain"
	"leadership-journal/internal/store"
)

func (s *Server) registerEntries(rt router) {
	rt.protected("POST /entries", s.entryCreate)
	rt.protected("GET /entries/{id}/edit", s.entryEdit)
	rt.protected("POST /entries/{id}", s.entryUpdate)
	rt.protected("POST /entries/{id}/finalize", s.entryFinalize)
	rt.protected("POST /entries/{id}/void", s.entryVoid)
	rt.protected("POST /entries/{id}/delete", s.entryDelete)
}

func (s *Server) entryCreate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	personID, err := formID(r, "person_id")
	if err != nil {
		return err
	}
	meetingID, err := formIDPtr(r, "meeting_id")
	if err != nil {
		return err
	}
	// An entry made in a meeting happened when the meeting did.
	when := s.now()
	if meetingID != nil {
		m, err := s.store.GetMeeting(ctx, *meetingID)
		if err != nil {
			return err
		}
		when = m.StartsAt
	}
	if when, err = s.occurredAt(r, "occurred_at", when); err != nil {
		return err
	}
	id, err := s.store.CreateEntry(ctx, domain.Entry{
		PersonID: personID, MeetingID: meetingID, Kind: formStr(r, "kind"), Body: formStr(r, "body"),
		Outcome: domain.Outcome(formStr(r, "outcome")), OccurredAt: when, Tags: parseTags(r.PostFormValue("tags")),
	})
	if err != nil {
		return err
	}
	s.log.Info("entry created", "entry_id", id, "person_id", personID, "kind", formStr(r, "kind"), "request_id", requestID(ctx))
	return s.done(w, r, fmt.Sprintf("/people/%d", personID), "Entry saved as a draft.")
}

type entryEditPage struct {
	Entry    domain.Entry
	Kinds    []domain.EntryKind
	Tags     []string
	TagsText string
	Date     string
	Back     string
}

func (s *Server) entryEdit(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		return err
	}
	e, err := s.store.GetEntry(ctx, id)
	if err != nil {
		return err
	}
	if !e.IsDraft() {
		return store.ErrNotDraft
	}
	kinds, _, err := s.entryKindLabels(ctx)
	if err != nil {
		return err
	}
	tags, err := s.store.ListTags(ctx)
	if err != nil {
		return err
	}
	back := r.URL.Query().Get("back")
	if !isLocalPath(back) {
		back = fmt.Sprintf("/people/%d", e.PersonID)
	}
	return s.render(w, r, http.StatusOK, "entry_edit", "people", entryEditPage{
		Entry: e, Kinds: kinds, Tags: tags, TagsText: strings.Join(e.Tags, ", "),
		Date: e.OccurredAt.Format(dateLayout), Back: back,
	})
}

func (s *Server) entryUpdate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		return err
	}
	e, err := s.store.GetEntry(ctx, id)
	if err != nil {
		return err
	}
	e.Kind = formStr(r, "kind")
	e.Body = formStr(r, "body")
	e.Outcome = domain.Outcome(formStr(r, "outcome"))
	e.Tags = parseTags(r.PostFormValue("tags"))
	if e.OccurredAt, err = s.occurredAt(r, "occurred_at", e.OccurredAt); err != nil {
		return err
	}
	if err := s.store.UpdateEntry(ctx, e); err != nil {
		return err
	}
	return s.done(w, r, fmt.Sprintf("/people/%d", e.PersonID), "Entry updated.")
}

func (s *Server) entryFinalize(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.store.FinalizeEntry(r.Context(), id); err != nil {
		return err
	}
	s.log.Info("entry finalized", "entry_id", id, "request_id", requestID(r.Context()))
	return s.done(w, r, "/", "Entry finalized. It is now part of the record.")
}

func (s *Server) entryVoid(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.store.VoidEntry(r.Context(), id, formStr(r, "reason")); err != nil {
		return err
	}
	s.log.Info("entry voided", "entry_id", id, "request_id", requestID(r.Context()))
	return s.done(w, r, "/", "Entry voided. It stays on record, marked as wrong.")
}

func (s *Server) entryDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.store.DeleteDraftEntry(r.Context(), id); err != nil {
		return err
	}
	s.log.Info("draft entry deleted", "entry_id", id, "request_id", requestID(r.Context()))
	return s.done(w, r, "/", "Draft deleted.")
}
