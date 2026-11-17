package web

import (
	"fmt"
	"net/http"
	"strings"

	"leadership-journal/internal/domain"
	"leadership-journal/internal/store"
)

func (s *Server) registerDrafts(rt router) {
	rt.protected("GET /drafts", s.draftsList)
	rt.protected("POST /drafts/finalize", s.draftsFinalize)
}

// excerptRunes is how much of a note the drafts list shows.
const excerptRunes = 120

type draftMeetingRow struct {
	meetingRow
	Noted int
}

type draftEntryRow struct {
	Entry      domain.Entry
	KindLabel  string
	PersonName string
	Excerpt    string
}

type draftCommitmentRow struct {
	C          domain.Commitment
	PersonName string
}

type draftsPage struct {
	Meetings    []draftMeetingRow
	Entries     []draftEntryRow
	Commitments []draftCommitmentRow
	Work        []workRow
	Empty       bool
}

// draftsList shows everything not finalized yet, so it can be reviewed and
// finalized in one go.
func (s *Server) draftsList(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	d, err := s.store.ListDrafts(ctx)
	if err != nil {
		return err
	}
	names, err := s.personNames(ctx)
	if err != nil {
		return err
	}
	_, labels, err := s.entryKindLabels(ctx)
	if err != nil {
		return err
	}
	releases, err := s.store.ListReleases(ctx)
	if err != nil {
		return err
	}
	releaseNames := make(map[int64]string, len(releases))
	for _, rel := range releases {
		releaseNames[rel.ID] = rel.Name
	}

	rows, err := s.meetingRows(ctx, d.Meetings)
	if err != nil {
		return err
	}
	var page draftsPage
	for _, row := range rows {
		page.Meetings = append(page.Meetings, draftMeetingRow{meetingRow: row, Noted: d.InMeeting[row.Meeting.ID]})
	}
	for _, e := range d.Entries {
		page.Entries = append(page.Entries, draftEntryRow{
			Entry: e, KindLabel: label(labels, e.Kind), PersonName: names[e.PersonID], Excerpt: excerpt(e.Body, excerptRunes),
		})
	}
	for _, c := range d.Commitments {
		page.Commitments = append(page.Commitments, draftCommitmentRow{C: c, PersonName: names[c.PersonID]})
	}
	for _, it := range d.WorkItems {
		page.Work = append(page.Work, workRow{Item: it, ReleaseName: releaseNames[it.ReleaseID], PersonName: names[it.PersonID]})
	}
	page.Empty = len(page.Meetings)+len(page.Entries)+len(page.Commitments)+len(page.Work) == 0
	return s.render(w, r, http.StatusOK, "drafts", "drafts", page)
}

func (s *Server) draftsFinalize(w http.ResponseWriter, r *http.Request) error {
	var sel store.DraftSelection
	var err error
	for _, f := range []struct {
		field string
		dst   *[]int64
	}{{"meeting", &sel.Meetings}, {"entry", &sel.Entries}, {"commitment", &sel.Commitments}, {"task", &sel.WorkItems}} {
		if *f.dst, err = formIDs(r, f.field); err != nil {
			return err
		}
	}
	if sel.Len() == 0 {
		return userErrorf("Select at least one draft to finalize")
	}
	if err := s.store.FinalizeDrafts(r.Context(), sel); err != nil {
		return err
	}
	s.log.Info("drafts finalized", "meeting_ids", sel.Meetings, "entry_ids", sel.Entries,
		"commitment_ids", sel.Commitments, "work_item_ids", sel.WorkItems, "request_id", requestID(r.Context()))
	noun := "records"
	if sel.Len() == 1 {
		noun = "record"
	}
	return s.redirect(w, r, "/drafts", fmt.Sprintf("Finalized %d %s. They are now part of the record.", sel.Len(), noun))
}

// excerpt is the start of a text on one line, cut to at most n runes.
func excerpt(text string, n int) string {
	one := strings.Join(strings.Fields(text), " ")
	runes := []rune(one)
	if len(runes) <= n {
		return one
	}
	return strings.TrimSpace(string(runes[:n])) + "…"
}
