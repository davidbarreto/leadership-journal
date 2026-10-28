package web

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	texttemplate "text/template"
	"time"

	"leadership-journal/internal/domain"
	"leadership-journal/internal/store"
)

func (s *Server) registerReport(rt router) {
	rt.protected("GET /people/{id}/report", s.report)
}

func loadReportTemplate() (*texttemplate.Template, error) {
	return texttemplate.New("report.md").Funcs(texttemplate.FuncMap(funcMap())).ParseFS(assets, "templates/report.md")
}

type reportSections struct {
	Entries, Commitments, Work, Progress bool
}

type kindOption struct {
	Code, Label string
	Checked     bool
}

type reportEntry struct {
	Entry     domain.Entry
	KindLabel string
}

type reportPage struct {
	Person           domain.Person
	TeamName         string
	Level            string
	FromInput        string
	ToInput          string
	HasPeriod        bool
	Sections         reportSections
	KindOptions      []kindOption
	Entries          []reportEntry
	Checks           []domain.Entry
	NoProgressStreak int
	Commitments      []domain.Commitment
	Work             []workRow
	Metrics          domain.WorkMetrics
	Standings        []releaseStanding
	Generated        time.Time
	MarkdownLink     string
}

// queryDate reads an optional date from the query string.
func (s *Server) queryDate(q url.Values, key string) (*time.Time, error) {
	v := strings.TrimSpace(q.Get(key))
	if v == "" {
		return nil, nil
	}
	t, err := time.ParseInLocation(dateLayout, v, s.loc)
	if err != nil {
		return nil, userErrorf("Invalid date: use the form YYYY-MM-DD")
	}
	return &t, nil
}

// inPeriod reports whether the calendar day of t lies within [from, to];
// a nil bound is open.
func inPeriod(t time.Time, from, to *time.Time) bool {
	day := func(x time.Time) time.Time {
		y, m, d := x.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	d := day(t)
	if from != nil && d.Before(day(*from)) {
		return false
	}
	if to != nil && d.After(day(*to)) {
		return false
	}
	return true
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// report builds what the owner can share with their manager. It holds only
// finalized, non-voided records of someone who reports to them, so drafts and
// private notes about the owner or their manager can never leak into it, and
// the owner picks the period, sections and kinds of note to include.
func (s *Server) report(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		return err
	}
	person, err := s.store.GetPerson(ctx, id)
	if err != nil {
		return err
	}
	if person.Relationship != domain.RelationshipReport {
		return &httpError{http.StatusForbidden, "Reports can only be generated for people who report to you."}
	}

	q := r.URL.Query()
	from, err := s.queryDate(q, "from")
	if err != nil {
		return err
	}
	to, err := s.queryDate(q, "to")
	if err != nil {
		return err
	}

	// Before the options form is first submitted, everything is included.
	submitted := q.Get("submitted") != ""
	page := reportPage{Person: person, Generated: s.now(), HasPeriod: from != nil || to != nil}
	if from != nil {
		page.FromInput = from.Format(dateLayout)
	}
	if to != nil {
		page.ToInput = to.Format(dateLayout)
	}
	chosen := q["sections"]
	page.Sections = reportSections{
		Entries:     !submitted || contains(chosen, "entries"),
		Commitments: !submitted || contains(chosen, "commitments"),
		Work:        !submitted || contains(chosen, "work"),
		Progress:    !submitted || contains(chosen, "progress"),
	}

	kinds, labels, err := s.entryKindLabels(ctx)
	if err != nil {
		return err
	}
	outcomeKinds := map[string]bool{}
	selected := map[string]bool{}
	for _, k := range kinds {
		if k.RequiresOutcome {
			outcomeKinds[k.Code] = true
			continue
		}
		on := !submitted || contains(q["kind"], k.Code)
		selected[k.Code] = on
		page.KindOptions = append(page.KindOptions, kindOption{Code: k.Code, Label: k.Label, Checked: on})
	}

	if err := s.fillReport(ctx, &page, id, from, to, labels, outcomeKinds, selected); err != nil {
		return err
	}
	page.TeamName, page.Level = s.reportHeader(r, person)

	link := url.Values{}
	for k, v := range q {
		link[k] = v
	}
	link.Set("format", "md")
	page.MarkdownLink = fmt.Sprintf("/people/%d/report?%s", id, link.Encode())

	if q.Get("format") == "md" {
		return s.reportMarkdown(w, page)
	}
	return s.render(w, r, http.StatusOK, "report", "people", page)
}

func (s *Server) fillReport(ctx context.Context, page *reportPage, id int64, from, to *time.Time,
	labels map[string]string, outcomeKinds, selected map[string]bool) error {
	entries, err := s.store.ListEntries(ctx, store.EntryFilter{PersonID: &id, FinalizedOnly: true})
	if err != nil {
		return err
	}
	// The store returns newest first; a report reads oldest first.
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if !inPeriod(e.OccurredAt, from, to) {
			continue
		}
		switch {
		case outcomeKinds[e.Kind]:
			if page.Sections.Progress {
				page.Checks = append(page.Checks, e)
			}
		case selected[e.Kind]:
			if page.Sections.Entries {
				page.Entries = append(page.Entries, reportEntry{Entry: e, KindLabel: label(labels, e.Kind)})
			}
		}
	}
	if page.Sections.Progress {
		outcomes, err := s.store.ProgressOutcomes(ctx, id)
		if err != nil {
			return err
		}
		page.NoProgressStreak = domain.ConsecutiveNoProgress(outcomes)
	}

	if page.Sections.Commitments {
		list, err := s.store.ListCommitments(ctx, store.CommitmentFilter{PersonID: &id, FinalizedOnly: true})
		if err != nil {
			return err
		}
		for _, c := range list {
			if inPeriod(c.CreatedAt, from, to) {
				page.Commitments = append(page.Commitments, c)
			}
		}
	}

	if page.Sections.Work {
		items, err := s.store.ListWorkItems(ctx, store.WorkFilter{PersonID: &id})
		if err != nil {
			return err
		}
		releases, err := s.store.ListReleases(ctx)
		if err != nil {
			return err
		}
		names := map[int64]string{}
		for _, rel := range releases {
			names[rel.ID] = rel.Name
		}
		var inScope []domain.WorkItem
		perRelease := map[int64][]domain.WorkItem{}
		for _, it := range items {
			if !it.IsFinalized() || !inPeriod(it.AssignedAt, from, to) {
				continue
			}
			inScope = append(inScope, it)
			perRelease[it.ReleaseID] = append(perRelease[it.ReleaseID], it)
			page.Work = append(page.Work, workRow{Item: it, ReleaseName: names[it.ReleaseID]})
		}
		page.Metrics = domain.ComputeWorkMetrics(inScope)
		for _, rel := range releases {
			if group, ok := perRelease[rel.ID]; ok {
				page.Standings = append(page.Standings, releaseStanding{Release: rel, Compliant: domain.ReleaseCompliant(group), Items: len(group)})
			}
		}
	}
	return nil
}

// reportHeader resolves the team name and current level shown in the heading.
func (s *Server) reportHeader(r *http.Request, p domain.Person) (team, level string) {
	ctx := r.Context()
	if p.TeamID != nil {
		if teams, err := s.store.ListTeams(ctx); err == nil {
			for _, t := range teams {
				if t.ID == *p.TeamID {
					team = t.Name
				}
			}
		}
	}
	level, _ = s.currentLevelCode(ctx, p.ID, s.now())
	return team, level
}

func (s *Server) reportMarkdown(w http.ResponseWriter, page reportPage) error {
	var buf bytes.Buffer
	if err := s.reportMD.Execute(&buf, page); err != nil {
		return fmt.Errorf("render markdown report: %w", err)
	}
	slug := strings.ReplaceAll(domain.CodeFromLabel(page.Person.Name), "_", "-")
	if slug == "" {
		slug = "person"
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report-%s.md"`, slug))
	_, err := buf.WriteTo(w)
	return err
}
