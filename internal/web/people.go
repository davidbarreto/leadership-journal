package web

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"leadership-journal/internal/domain"
	"leadership-journal/internal/store"
)

// documentationTag marks notes that record a documentation contribution; the
// count of finalized ones is one of the metrics of the process.
const documentationTag = "documentation"

func (s *Server) registerPeople(rt router) {
	rt.protected("GET /people", s.peopleList)
	rt.protected("GET /people/new", s.personNew)
	rt.protected("POST /people", s.personCreate)
	rt.protected("GET /people/{id}", s.personShow)
	rt.protected("GET /people/{id}/edit", s.personEdit)
	rt.protected("POST /people/{id}", s.personUpdate)
	rt.protected("POST /people/{id}/level", s.personLevel)
}

type personRow struct {
	Person   domain.Person
	TeamName string
	Level    string
	Progress *store.PersonProgress
	Start    *quickMeeting
}

type peopleListPage struct {
	Reports, Managers, Selves []personRow
	Empty                     bool
}

func (s *Server) peopleList(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	people, err := s.store.ListPeople(ctx, "")
	if err != nil {
		return err
	}
	teams, err := s.store.ListTeams(ctx)
	if err != nil {
		return err
	}
	teamNames := map[int64]string{}
	for _, t := range teams {
		teamNames[t.ID] = t.Name
	}
	progress, err := s.store.ProgressStatuses(ctx, s.now(), s.progressDays)
	if err != nil {
		return err
	}
	progressByID := map[int64]*store.PersonProgress{}
	for i := range progress {
		progressByID[progress[i].Person.ID] = &progress[i]
	}

	kind, hasKind, err := s.quickMeetingKind(ctx)
	if err != nil {
		return err
	}
	page := peopleListPage{Empty: len(people) == 0}
	for _, p := range people {
		level, err := s.currentLevelCode(ctx, p.ID, s.now())
		if err != nil {
			return err
		}
		row := personRow{Person: p, Level: level, Progress: progressByID[p.ID], Start: quickMeetingFor(p, kind, hasKind, csrfOf(r))}
		if p.TeamID != nil {
			row.TeamName = teamNames[*p.TeamID]
		}
		switch p.Relationship {
		case domain.RelationshipReport:
			page.Reports = append(page.Reports, row)
		case domain.RelationshipManager:
			page.Managers = append(page.Managers, row)
		default:
			page.Selves = append(page.Selves, row)
		}
	}
	return s.render(w, r, http.StatusOK, "people", "people", page)
}

type personFormPage struct {
	Title  string
	Action string
	Person domain.Person
	Teams  []domain.Team
}

func (s *Server) personNew(w http.ResponseWriter, r *http.Request) error {
	teams, err := s.store.ListTeams(r.Context())
	if err != nil {
		return err
	}
	return s.render(w, r, http.StatusOK, "person_form", "people", personFormPage{
		Title: "Add a person", Action: "/people", Teams: teams,
		Person: domain.Person{Relationship: domain.RelationshipReport},
	})
}

func personFromForm(r *http.Request) (domain.Person, error) {
	teamID, err := formIDPtr(r, "team_id")
	if err != nil {
		return domain.Person{}, err
	}
	return domain.Person{
		Name:         formStr(r, "name"),
		Relationship: domain.Relationship(formStr(r, "relationship")),
		RoleTitle:    formStr(r, "role_title"),
		TeamID:       teamID,
	}, nil
}

func (s *Server) personCreate(w http.ResponseWriter, r *http.Request) error {
	p, err := personFromForm(r)
	if err != nil {
		return err
	}
	id, err := s.store.CreatePerson(r.Context(), p)
	if err != nil {
		return err
	}
	s.log.Info("person created", "person_id", id, "relationship", p.Relationship, "request_id", requestID(r.Context()))
	return s.redirect(w, r, fmt.Sprintf("/people/%d", id), "Person added.")
}

func (s *Server) personEdit(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	p, err := s.store.GetPerson(r.Context(), id)
	if err != nil {
		return err
	}
	teams, err := s.store.ListTeams(r.Context())
	if err != nil {
		return err
	}
	return s.render(w, r, http.StatusOK, "person_form", "people", personFormPage{
		Title: "Edit " + p.Name, Action: fmt.Sprintf("/people/%d", id), Person: p, Teams: teams,
	})
}

func (s *Server) personUpdate(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	p, err := personFromForm(r)
	if err != nil {
		return err
	}
	p.ID = id
	if err := s.store.UpdatePerson(r.Context(), p); err != nil {
		return err
	}
	return s.redirect(w, r, fmt.Sprintf("/people/%d", id), "Person updated.")
}

func (s *Server) personLevel(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	levelID, err := formID(r, "level_id")
	if err != nil {
		return err
	}
	from, err := s.formDate(r, "effective_from")
	if err != nil {
		return err
	}
	if from.IsZero() {
		return userErrorf("Date is required")
	}
	if err := s.store.AssignLevel(r.Context(), id, levelID, from); err != nil {
		return err
	}
	return s.done(w, r, personTabURL(id, "level"), "Level recorded.")
}

type levelRow struct {
	Code, Title string
	From        time.Time
}

type releaseStanding struct {
	Release   domain.Release
	Compliant bool
	Items     int
}

type workRow struct {
	Item        domain.WorkItem
	ReleaseName string
	PersonName  string
}

// personTab is one tab of the person page.
type personTab struct {
	Code, Label string
}

// personTabs are the person page's tabs in display order; the first is the
// default.
var personTabs = []personTab{
	{"overview", "Overview"},
	{"notes", "Notes"},
	{"commitments", "Commitments"},
	{"meetings", "Meetings"},
	{"work", "Work"},
	{"level", "Level"},
}

// personTabFrom picks the requested tab, falling back to the overview for an
// unknown or missing one.
func personTabFrom(r *http.Request) string {
	want := r.URL.Query().Get("tab")
	for _, t := range personTabs {
		if t.Code == want {
			return want
		}
	}
	return personTabs[0].Code
}

// personTabURL is where a tab lives, and where actions posted from it return.
func personTabURL(personID int64, tab string) string {
	if tab == personTabs[0].Code {
		return fmt.Sprintf("/people/%d", personID)
	}
	return fmt.Sprintf("/people/%d?tab=%s", personID, tab)
}

// overviewLimits keep the overview short; the tabs hold the full lists.
const (
	overviewCommitments = 5
	overviewNotes       = 3
)

// personOverview is the summary shown on the first tab.
type personOverview struct {
	OpenCommitments []commitmentView
	MoreCommitments int
	LatestNotes     []entryView
	LastMeeting     *meetingRow
	Release         *domain.Release
	Standing        *releaseStanding
}

// personPage is the person page: a header shown on every tab, then the data
// of the selected tab only.
type personPage struct {
	Person   domain.Person
	TeamName string
	Level    string
	Progress *store.PersonProgress
	Start    *quickMeeting
	Tabs     []personTab
	Tab      string
	// AddOpen unfolds the tab's add form.
	AddOpen bool
	Back    string
	CSRF    string

	Overview    personOverview
	Entries     []entryView
	EntryForm   entryFormData
	Commitments []commitmentView
	CommitForm  commitmentFormData
	Meetings    []meetingRow
	Work        []workRow
	Metrics     domain.WorkMetrics
	Standings   []releaseStanding
	DocCount    int
	TaskForm    taskFormData
	CanAddTask  bool
	History     []levelRow
	Levels      []domain.Level
}

func (s *Server) personShow(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		return err
	}
	p, err := s.store.GetPerson(ctx, id)
	if err != nil {
		return err
	}
	tab := personTabFrom(r)
	page := personPage{
		Person: p, Tabs: personTabs, Tab: tab, AddOpen: r.URL.Query().Get("add") == "1",
		Back: personTabURL(id, tab), CSRF: csrfOf(r),
	}
	if err := s.loadPersonHeader(ctx, &page); err != nil {
		return err
	}

	var load func(context.Context, *http.Request, *personPage) error
	switch tab {
	case "notes":
		load = s.loadNotesTab
	case "commitments":
		load = s.loadCommitmentsTab
	case "meetings":
		load = s.loadMeetingsTab
	case "work":
		load = s.loadWorkTab
	case "level":
		load = s.loadLevelTab
	default:
		load = s.loadOverviewTab
	}
	if err := load(ctx, r, &page); err != nil {
		return err
	}
	return s.render(w, r, http.StatusOK, "person", "people", page)
}

// loadPersonHeader fills what every tab shows: team, current level and, for
// reports, the improvement journal status.
func (s *Server) loadPersonHeader(ctx context.Context, page *personPage) error {
	p := page.Person
	if p.TeamID != nil {
		teams, err := s.store.ListTeams(ctx)
		if err != nil {
			return err
		}
		for _, t := range teams {
			if t.ID == *p.TeamID {
				page.TeamName = t.Name
			}
		}
	}
	level, err := s.currentLevelCode(ctx, p.ID, s.now())
	if err != nil {
		return err
	}
	page.Level = level

	if p.Relationship == domain.RelationshipReport {
		last, err := s.store.LastProgressCheck(ctx, p.ID)
		if err != nil {
			return err
		}
		outcomes, err := s.store.ProgressOutcomes(ctx, p.ID)
		if err != nil {
			return err
		}
		page.Progress = &store.PersonProgress{
			Person: p, LastCheck: last, Overdue: domain.ProgressOverdue(last, s.now(), s.progressDays), IntervalDays: s.progressDays,
			NoProgressStreak: domain.ConsecutiveNoProgress(outcomes),
		}
	}
	kind, ok, err := s.quickMeetingKind(ctx)
	if err != nil {
		return err
	}
	page.Start = quickMeetingFor(p, kind, ok, page.CSRF)
	return nil
}

func (s *Server) loadOverviewTab(ctx context.Context, r *http.Request, page *personPage) error {
	id := page.Person.ID
	names := map[int64]string{id: page.Person.Name}
	open, err := s.store.ListCommitments(ctx, store.CommitmentFilter{PersonID: &id, OnlyOpen: true})
	if err != nil {
		return err
	}
	if len(open) > overviewCommitments {
		page.Overview.MoreCommitments = len(open) - overviewCommitments
		open = open[:overviewCommitments]
	}
	page.Overview.OpenCommitments = s.commitmentViews(open, names, page.Back, r)

	_, labels, err := s.entryKindLabels(ctx)
	if err != nil {
		return err
	}
	notes, err := s.store.ListEntries(ctx, store.EntryFilter{PersonID: &id, Limit: overviewNotes})
	if err != nil {
		return err
	}
	page.Overview.LatestNotes = s.entryViews(notes, labels, page.Back, r)

	meetings, err := s.store.ListMeetings(ctx, store.MeetingFilter{PersonID: &id, Limit: 1})
	if err != nil {
		return err
	}
	rows, err := s.meetingRows(ctx, meetings)
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		page.Overview.LastMeeting = &rows[0]
	}

	if page.Person.Relationship != domain.RelationshipReport {
		return nil
	}
	releases, err := s.store.ListReleases(ctx)
	if err != nil {
		return err
	}
	rel := currentRelease(releases, s.now())
	if rel == nil {
		return nil
	}
	items, err := s.store.ListWorkItems(ctx, store.WorkFilter{PersonID: &id, ReleaseID: &rel.ID})
	if err != nil {
		return err
	}
	page.Overview.Release = rel
	page.Overview.Standing = &releaseStanding{Release: *rel, Compliant: domain.ReleaseCompliant(items), Items: len(items)}
	return nil
}

func (s *Server) loadNotesTab(ctx context.Context, r *http.Request, page *personPage) error {
	id := page.Person.ID
	kinds, labels, err := s.entryKindLabels(ctx)
	if err != nil {
		return err
	}
	entries, err := s.store.ListEntries(ctx, store.EntryFilter{PersonID: &id, IncludeVoided: true})
	if err != nil {
		return err
	}
	page.Entries = s.entryViews(entries, labels, page.Back, r)
	tags, err := s.store.ListTags(ctx)
	if err != nil {
		return err
	}
	page.EntryForm = entryFormData{PersonID: id, Kinds: kinds, Tags: tags, Today: s.now().Format(dateLayout), Back: page.Back, CSRF: page.CSRF}
	page.AddOpen = page.AddOpen || len(entries) == 0
	return nil
}

func (s *Server) loadCommitmentsTab(ctx context.Context, r *http.Request, page *personPage) error {
	id := page.Person.ID
	commitments, err := s.store.ListCommitments(ctx, store.CommitmentFilter{PersonID: &id, IncludeVoided: true})
	if err != nil {
		return err
	}
	page.Commitments = s.commitmentViews(commitments, map[int64]string{id: page.Person.Name}, page.Back, r)
	page.CommitForm = commitmentFormData{PersonID: id, Back: page.Back, CSRF: page.CSRF}
	page.AddOpen = page.AddOpen || len(commitments) == 0
	return nil
}

func (s *Server) loadMeetingsTab(ctx context.Context, _ *http.Request, page *personPage) error {
	meetings, err := s.store.ListMeetings(ctx, store.MeetingFilter{PersonID: &page.Person.ID, IncludeVoided: true})
	if err != nil {
		return err
	}
	page.Meetings, err = s.meetingRows(ctx, meetings)
	return err
}

func (s *Server) loadWorkTab(ctx context.Context, _ *http.Request, page *personPage) error {
	id := page.Person.ID
	releases, err := s.fillWork(ctx, page, id)
	if err != nil {
		return err
	}
	// Tasks are only for reports, as on the work board.
	if page.Person.Relationship == domain.RelationshipReport && len(releases) > 0 {
		page.CanAddTask = true
		page.TaskForm = taskFormData{
			Action: "/work", Item: domain.WorkItem{PersonID: id, ReleaseID: currentRelease(releases, s.now()).ID},
			AssignedAt: s.now().Format(dateLayout), Releases: releases, FixedPerson: true,
			Submit: "Add as draft", Back: page.Back, CSRF: page.CSRF,
		}
		page.AddOpen = page.AddOpen || len(page.Work) == 0
	}
	docs, err := s.store.ListEntries(ctx, store.EntryFilter{PersonID: &id, Tag: documentationTag, FinalizedOnly: true})
	if err != nil {
		return err
	}
	page.DocCount = len(docs)
	return nil
}

func (s *Server) loadLevelTab(ctx context.Context, _ *http.Request, page *personPage) error {
	history, err := s.store.LevelHistory(ctx, page.Person.ID)
	if err != nil {
		return err
	}
	for _, a := range history {
		page.History = append(page.History, levelRow{Code: a.Level.Code, Title: a.Level.Title, From: a.EffectiveFrom})
	}
	if page.Levels, err = s.store.ListLevels(ctx); err != nil {
		return err
	}
	page.AddOpen = page.AddOpen || len(history) == 0
	return nil
}

// fillWork adds the person's tasks, metrics and per-release standing, and
// returns all releases, newest first.
func (s *Server) fillWork(ctx context.Context, page *personPage, personID int64) ([]domain.Release, error) {
	items, err := s.store.ListWorkItems(ctx, store.WorkFilter{PersonID: &personID})
	if err != nil {
		return nil, err
	}
	releases, err := s.store.ListReleases(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[int64]domain.Release{}
	for _, rel := range releases {
		byID[rel.ID] = rel
	}
	perRelease := map[int64][]domain.WorkItem{}
	for _, it := range items {
		page.Work = append(page.Work, workRow{Item: it, ReleaseName: byID[it.ReleaseID].Name})
		perRelease[it.ReleaseID] = append(perRelease[it.ReleaseID], it)
	}
	page.Metrics = domain.ComputeWorkMetrics(items)
	for _, rel := range releases {
		if group, ok := perRelease[rel.ID]; ok {
			page.Standings = append(page.Standings, releaseStanding{Release: rel, Compliant: domain.ReleaseCompliant(group), Items: len(group)})
		}
	}
	return releases, nil
}
