package web

import (
	"fmt"
	"net/http"

	"leadership-journal/internal/domain"
)

func (s *Server) registerCommitments(rt router) {
	rt.protected("POST /commitments", s.commitmentCreate)
	rt.protected("POST /commitments/{id}/status", s.commitmentStatus)
	rt.protected("POST /commitments/{id}/finalize", s.commitmentFinalize)
	rt.protected("POST /commitments/{id}/void", s.commitmentVoid)
	rt.protected("POST /commitments/{id}/delete", s.commitmentDelete)
}

func (s *Server) commitmentCreate(w http.ResponseWriter, r *http.Request) error {
	personID, err := formID(r, "person_id")
	if err != nil {
		return err
	}
	meetingID, err := formIDPtr(r, "meeting_id")
	if err != nil {
		return err
	}
	due, err := s.formDatePtr(r, "due_on")
	if err != nil {
		return err
	}
	id, err := s.store.CreateCommitment(r.Context(), domain.Commitment{
		PersonID: personID, MeetingID: meetingID,
		Kind:      domain.CommitmentKind(formStr(r, "kind")),
		Direction: domain.Direction(formStr(r, "direction")),
		Content:   formStr(r, "content"), Deliverable: formStr(r, "deliverable"), DueOn: due,
	})
	if err != nil {
		return err
	}
	s.log.Info("commitment created", "commitment_id", id, "person_id", personID, "request_id", requestID(r.Context()))
	return s.done(w, r, fmt.Sprintf("/people/%d", personID), "Commitment saved as a draft.")
}

func (s *Server) commitmentStatus(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	status := domain.CommitmentStatus(formStr(r, "status"))
	if err := s.store.SetCommitmentStatus(r.Context(), id, status, formStr(r, "note")); err != nil {
		return err
	}
	s.log.Info("commitment status changed", "commitment_id", id, "status", status, "request_id", requestID(r.Context()))
	return s.done(w, r, "/", "Status updated.")
}

func (s *Server) commitmentFinalize(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.store.FinalizeCommitment(r.Context(), id); err != nil {
		return err
	}
	return s.done(w, r, "/", "Commitment finalized. It is now part of the record.")
}

func (s *Server) commitmentVoid(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.store.VoidCommitment(r.Context(), id, formStr(r, "reason")); err != nil {
		return err
	}
	return s.done(w, r, "/", "Commitment voided. It stays on record, marked as wrong.")
}

func (s *Server) commitmentDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.store.DeleteDraftCommitment(r.Context(), id); err != nil {
		return err
	}
	return s.done(w, r, "/", "Draft deleted.")
}
