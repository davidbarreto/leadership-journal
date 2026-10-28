package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"leadership-journal/internal/domain"
	"leadership-journal/internal/store"
)

func (s *Server) registerWork(rt router) {
	rt.protected("GET /work", s.workBoard)
	rt.protected("POST /releases", s.releaseCreate)
	rt.protected("POST /work", s.workCreate)
	rt.protected("GET /work/{id}", s.workShow)
	rt.protected("POST /work/{id}", s.workUpdate)
	rt.protected("POST /work/{id}/tracking", s.workTracking)
	rt.protected("POST /work/{id}/move", s.workMove)
	rt.protected("POST /work/{id}/finalize", s.workFinalize)
	rt.protected("POST /work/{id}/void", s.workVoid)
	rt.protected("POST /work/{id}/delete", s.workDelete)
}

type workBoardPage struct {
	Releases   []domain.Release
	Selected   *domain.Release
	Compliance []store.ComplianceRow
	Rows       []workRow
	People     []domain.Person
	Today      string
}

// currentRelease picks the release running at the given time, or else the
// newest one. Releases are listed newest first.
func currentRelease(releases []domain.Release, at time.Time) *domain.Release {
	for i, rel := range releases {
		if !at.Before(rel.StartsOn) && !at.After(rel.EndsOn.AddDate(0, 0, 1)) {
			return &releases[i]
		}
	}
	if len(releases) > 0 {
		return &releases[0]
	}
	return nil
}

func (s *Server) workBoard(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	releases, err := s.store.ListReleases(ctx)
	if err != nil {
		return err
	}
	page := workBoardPage{Releases: releases, Today: s.now().Format(dateLayout)}
	if q := r.URL.Query().Get("release"); q != "" {
		id, err := strconv.ParseInt(q, 10, 64)
		if err != nil || id <= 0 {
			return store.ErrNotFound
		}
		rel, err := s.store.GetRelease(ctx, id)
		if err != nil {
			return err
		}
		page.Selected = &rel
	} else {
		page.Selected = currentRelease(releases, s.now())
	}

	if page.Selected != nil {
		if page.Compliance, err = s.store.ReleaseCompliance(ctx, page.Selected.ID); err != nil {
			return err
		}
		items, err := s.store.ListWorkItems(ctx, store.WorkFilter{ReleaseID: &page.Selected.ID})
		if err != nil {
			return err
		}
		names, err := s.personNames(ctx)
		if err != nil {
			return err
		}
		for _, it := range items {
			page.Rows = append(page.Rows, workRow{Item: it, ReleaseName: page.Selected.Name, PersonName: names[it.PersonID]})
		}
	}
	if page.People, err = s.store.ListPeople(ctx, domain.RelationshipReport); err != nil {
		return err
	}
	return s.render(w, r, http.StatusOK, "work", "work", page)
}

func (s *Server) releaseCreate(w http.ResponseWriter, r *http.Request) error {
	starts, err := s.formDate(r, "starts_on")
	if err != nil {
		return err
	}
	ends, err := s.formDate(r, "ends_on")
	if err != nil {
		return err
	}
	if starts.IsZero() || ends.IsZero() {
		return userErrorf("Start and end dates are required")
	}
	id, err := s.store.CreateRelease(r.Context(), domain.Release{Name: formStr(r, "name"), StartsOn: starts, EndsOn: ends})
	if err != nil {
		return err
	}
	return s.redirect(w, r, fmt.Sprintf("/work?release=%d", id), "Release added.")
}

// workItemFromForm reads the judgment fields shared by create, update and move.
func (s *Server) workItemFromForm(r *http.Request) (domain.WorkItem, error) {
	personID, err := formID(r, "person_id")
	if err != nil {
		return domain.WorkItem{}, err
	}
	releaseID, err := formID(r, "release_id")
	if err != nil {
		return domain.WorkItem{}, err
	}
	assigned, err := s.formDate(r, "assigned_at")
	if err != nil {
		return domain.WorkItem{}, err
	}
	return domain.WorkItem{
		PersonID: personID, ReleaseID: releaseID, Title: formStr(r, "title"), ExternalRef: formStr(r, "external_ref"),
		PickedBy: domain.PickedBy(formStr(r, "picked_by")), Stretch: domain.Stretch(formStr(r, "stretch")),
		StretchRationale: formStr(r, "rationale"), AssignedAt: assigned,
	}, nil
}

func (s *Server) workCreate(w http.ResponseWriter, r *http.Request) error {
	item, err := s.workItemFromForm(r)
	if err != nil {
		return err
	}
	id, err := s.store.CreateWorkItem(r.Context(), item)
	if err != nil {
		return err
	}
	s.log.Info("work item created", "work_item_id", id, "person_id", item.PersonID, "stretch", item.Stretch, "request_id", requestID(r.Context()))
	return s.redirect(w, r, fmt.Sprintf("/work/%d", id), "Task saved as a draft. Finalize it to lock the stretch judgment.")
}

type workItemPage struct {
	Item        domain.WorkItem
	PersonName  string
	ReleaseName string
	Editable    bool
	Movable     bool
	Tracking    bool
	MovedFrom   *domain.WorkItem
	MovedTo     *domain.WorkItem
	People      []domain.Person
	Releases    []domain.Release
	AssignedAt  string
	CompletedAt string
	Today       string
	Back        string
}

func (s *Server) workShow(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		return err
	}
	item, err := s.store.GetWorkItem(ctx, id)
	if err != nil {
		return err
	}
	people, err := s.store.ListPeople(ctx, domain.RelationshipReport)
	if err != nil {
		return err
	}
	releases, err := s.store.ListReleases(ctx)
	if err != nil {
		return err
	}
	names, err := s.personNames(ctx)
	if err != nil {
		return err
	}

	live := !item.IsVoided() && item.Status != domain.WorkMoved
	page := workItemPage{
		Item: item, PersonName: names[item.PersonID], Editable: item.IsDraft(),
		Movable:  live && (item.Status == domain.WorkPlanned || item.Status == domain.WorkInProgress),
		Tracking: live, People: people, Releases: releases,
		AssignedAt: item.AssignedAt.Format(dateLayout), Today: s.now().Format(dateLayout), Back: fmt.Sprintf("/work/%d", id),
	}
	for _, rel := range releases {
		if rel.ID == item.ReleaseID {
			page.ReleaseName = rel.Name
		}
	}
	if item.CompletedAt != nil {
		page.CompletedAt = item.CompletedAt.Format(dateLayout)
	}
	if item.MovedFromID != nil {
		from, err := s.store.GetWorkItem(ctx, *item.MovedFromID)
		if err != nil {
			return err
		}
		page.MovedFrom = &from
	}
	if item.Status == domain.WorkMoved {
		siblings, err := s.store.ListWorkItems(ctx, store.WorkFilter{ReleaseID: &item.ReleaseID, IncludeVoided: true})
		if err != nil {
			return err
		}
		for i := range siblings {
			if siblings[i].MovedFromID != nil && *siblings[i].MovedFromID == id {
				page.MovedTo = &siblings[i]
			}
		}
	}
	return s.render(w, r, http.StatusOK, "work_item", "work", page)
}

func (s *Server) workUpdate(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	existing, err := s.store.GetWorkItem(r.Context(), id)
	if err != nil {
		return err
	}
	edited, err := s.workItemFromForm(r)
	if err != nil {
		return err
	}
	// Only the judgment fields come from the form; progress stays as it was.
	existing.PersonID, existing.ReleaseID, existing.Title, existing.ExternalRef = edited.PersonID, edited.ReleaseID, edited.Title, edited.ExternalRef
	existing.PickedBy, existing.Stretch, existing.StretchRationale = edited.PickedBy, edited.Stretch, edited.StretchRationale
	if !edited.AssignedAt.IsZero() {
		existing.AssignedAt = edited.AssignedAt
	}
	if err := s.store.UpdateWorkItem(r.Context(), existing); err != nil {
		return err
	}
	return s.redirect(w, r, fmt.Sprintf("/work/%d", id), "Task updated.")
}

func (s *Server) workTracking(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	completed, err := s.formDatePtr(r, "completed_at")
	if err != nil {
		return err
	}
	err = s.store.UpdateWorkTracking(r.Context(), id, store.WorkTracking{
		Status:            domain.WorkStatus(formStr(r, "status")),
		InvestigationDone: formStr(r, "investigation_done") == "1",
		InvestigationURL:  formStr(r, "investigation_url"),
		OutcomeNotes:      r.PostFormValue("outcome_notes"),
		CompletedAt:       completed,
	})
	if err != nil {
		return err
	}
	return s.redirect(w, r, fmt.Sprintf("/work/%d", id), "Progress updated.")
}

func (s *Server) workMove(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	to, err := s.workItemFromForm(r)
	if err != nil {
		return err
	}
	newID, err := s.store.MoveWorkItem(r.Context(), id, to)
	if err != nil {
		return err
	}
	s.log.Info("work item moved", "from_work_item_id", id, "to_work_item_id", newID, "request_id", requestID(r.Context()))
	return s.redirect(w, r, fmt.Sprintf("/work/%d", newID), "Task reassigned. The original is kept, marked as moved; finalize the new one to lock its judgment.")
}

func (s *Server) workFinalize(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.store.FinalizeWorkItem(r.Context(), id); err != nil {
		return err
	}
	s.log.Info("work item finalized", "work_item_id", id, "request_id", requestID(r.Context()))
	return s.redirect(w, r, fmt.Sprintf("/work/%d", id), "Task finalized. The stretch judgment is now locked.")
}

func (s *Server) workVoid(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.store.VoidWorkItem(r.Context(), id, formStr(r, "reason")); err != nil {
		return err
	}
	return s.redirect(w, r, fmt.Sprintf("/work/%d", id), "Task voided. It stays on record, marked as wrong.")
}

func (s *Server) workDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.store.DeleteDraftWorkItem(r.Context(), id); err != nil {
		return err
	}
	return s.redirect(w, r, "/work", "Draft deleted.")
}
