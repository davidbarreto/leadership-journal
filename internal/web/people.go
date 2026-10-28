package web

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"leadership-journal/internal/domain"
	"leadership-journal/internal/store"
)

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
	progress, err := s.store.ProgressStatuses(ctx, s.now())
	if err != nil {
		return err
	}
	progressByID := map[int64]*store.PersonProgress{}
	for i := range progress {
		progressByID[progress[i].Person.ID] = &progress[i]
	}

	page := peopleListPage{Empty: len(people) == 0}
	for _, p := range people {
		level, err := s.currentLevelCode(ctx, p.ID, s.now())
		if err != nil {
			return err
		}
		row := personRow{Person: p, Level: level, Progress: progressByID[p.ID]}
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
	return s.done(w, r, fmt.Sprintf("/people/%d", id), "Level recorded.")
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

type personPage struct {
	Person      domain.Person
	TeamName    string
	Level       string
	History     []levelRow
	Levels      []domain.Level
	Progress    *store.PersonProgress
	Entries     []entryView
	Commitments []commitmentView
	Meetings    []meetingRow
	Work        []workRow
	Metrics     domain.WorkMetrics
	Standings   []releaseStanding
	EntryForm   entryFormData
	CommitForm  commitmentFormData
	Back        string
	CSRF        string
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
	back := fmt.Sprintf("/people/%d", id)
	page := personPage{Person: p, Back: back, CSRF: csrfOf(r)}

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

	history, err := s.store.LevelHistory(ctx, id)
	if err != nil {
		return err
	}
	for _, a := range history {
		page.History = append(page.History, levelRow{Code: a.Level.Code, Title: a.Level.Title, From: a.EffectiveFrom})
	}
	if level, err := history.LevelAt(s.now()); err == nil {
		page.Level = level.Code
	}
	if page.Levels, err = s.store.ListLevels(ctx); err != nil {
		return err
	}

	if p.Relationship == domain.RelationshipReport {
		last, err := s.store.LastProgressCheck(ctx, id)
		if err != nil {
			return err
		}
		outcomes, err := s.store.ProgressOutcomes(ctx, id)
		if err != nil {
			return err
		}
		page.Progress = &store.PersonProgress{
			Person: p, LastCheck: last, Overdue: domain.ProgressOverdue(last, s.now()),
			NoProgressStreak: domain.ConsecutiveNoProgress(outcomes),
		}
	}

	kinds, labels, err := s.entryKindLabels(ctx)
	if err != nil {
		return err
	}
	entries, err := s.store.ListEntries(ctx, store.EntryFilter{PersonID: &id, IncludeVoided: true})
	if err != nil {
		return err
	}
	page.Entries = s.entryViews(entries, labels, back, r)

	tags, err := s.store.ListTags(ctx)
	if err != nil {
		return err
	}
	page.EntryForm = entryFormData{PersonID: id, Kinds: kinds, Tags: tags, Today: s.now().Format(dateLayout), Back: back, CSRF: csrfOf(r)}
	page.CommitForm = commitmentFormData{PersonID: id, Back: back, CSRF: csrfOf(r)}

	commitments, err := s.store.ListCommitments(ctx, store.CommitmentFilter{PersonID: &id, IncludeVoided: true})
	if err != nil {
		return err
	}
	page.Commitments = s.commitmentViews(commitments, map[int64]string{id: p.Name}, back, r)

	meetings, err := s.store.ListMeetings(ctx, store.MeetingFilter{PersonID: &id, Limit: 20})
	if err != nil {
		return err
	}
	page.Meetings, err = s.meetingRows(ctx, meetings)
	if err != nil {
		return err
	}

	if err := s.fillWork(ctx, &page, id); err != nil {
		return err
	}
	return s.render(w, r, http.StatusOK, "person", "people", page)
}

// fillWork adds the person's tasks, metrics and per-release standing.
func (s *Server) fillWork(ctx context.Context, page *personPage, personID int64) error {
	items, err := s.store.ListWorkItems(ctx, store.WorkFilter{PersonID: &personID})
	if err != nil {
		return err
	}
	releases, err := s.store.ListReleases(ctx)
	if err != nil {
		return err
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
	return nil
}
