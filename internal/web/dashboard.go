package web

import (
	"net/http"

	"leadership-journal/internal/domain"
	"leadership-journal/internal/store"
)

type dashboardPage struct {
	NoPeople    bool
	Drafts      store.DraftCounts
	AnyDrafts   bool
	Progress    []store.PersonProgress
	Commitments []commitmentView
	Release     *domain.Release
	Compliance  []store.ComplianceRow
}

// dashboard answers "what needs my attention?": drafts waiting to be locked,
// reports whose improvement journal is overdue or stuck, open commitments, and
// who has a stretch task in the running release.
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	people, err := s.store.ListPeople(ctx, "")
	if err != nil {
		return err
	}
	page := dashboardPage{NoPeople: len(people) == 0}

	if page.Drafts, err = s.store.PendingDrafts(ctx); err != nil {
		return err
	}
	d := page.Drafts
	page.AnyDrafts = d.Meetings+d.Entries+d.Commitments+d.WorkItems > 0

	if page.Progress, err = s.store.ProgressStatuses(ctx, s.now()); err != nil {
		return err
	}

	open, err := s.store.ListCommitments(ctx, store.CommitmentFilter{OnlyOpen: true})
	if err != nil {
		return err
	}
	names := make(map[int64]string, len(people))
	for _, p := range people {
		names[p.ID] = p.Name
	}
	page.Commitments = s.commitmentViews(open, names, "/", r)

	releases, err := s.store.ListReleases(ctx)
	if err != nil {
		return err
	}
	if page.Release = currentRelease(releases, s.now()); page.Release != nil {
		if page.Compliance, err = s.store.ReleaseCompliance(ctx, page.Release.ID); err != nil {
			return err
		}
	}
	return s.render(w, r, http.StatusOK, "dashboard", "dashboard", page)
}
