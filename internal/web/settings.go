package web

import (
	"net/http"
	"strconv"

	"leadership-journal/internal/domain"
)

func (s *Server) registerSettings(rt router) {
	rt.protected("GET /settings", s.settings)
	rt.protected("POST /settings/teams", s.settingsTeam)
	rt.protected("POST /settings/levels", s.settingsLevel)
	rt.protected("POST /settings/meeting-kinds", s.settingsMeetingKind)
	rt.protected("POST /settings/entry-kinds", s.settingsEntryKind)
	rt.protected("POST /settings/prompts", s.settingsPrompt)
}

type kindPrompts struct {
	Kind    domain.MeetingKind
	Prompts []domain.Prompt
}

type settingsPage struct {
	Teams        []domain.Team
	Levels       []domain.Level
	MeetingKinds []kindPrompts
	EntryKinds   []domain.EntryKind
	Tags         []string
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var page settingsPage
	var err error
	if page.Teams, err = s.store.ListTeams(ctx); err != nil {
		return err
	}
	if page.Levels, err = s.store.ListLevels(ctx); err != nil {
		return err
	}
	kinds, err := s.store.ListMeetingKinds(ctx)
	if err != nil {
		return err
	}
	for _, k := range kinds {
		prompts, err := s.store.ListPrompts(ctx, k.Code)
		if err != nil {
			return err
		}
		page.MeetingKinds = append(page.MeetingKinds, kindPrompts{Kind: k, Prompts: prompts})
	}
	if page.EntryKinds, err = s.store.ListEntryKinds(ctx); err != nil {
		return err
	}
	if page.Tags, err = s.store.ListTags(ctx); err != nil {
		return err
	}
	return s.render(w, r, http.StatusOK, "settings", "settings", page)
}

func (s *Server) settingsTeam(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.store.CreateTeam(r.Context(), formStr(r, "name")); err != nil {
		return err
	}
	return s.done(w, r, "/settings", "Team added.")
}

func (s *Server) settingsLevel(w http.ResponseWriter, r *http.Request) error {
	rank, err := strconv.Atoi(formStr(r, "rank"))
	if err != nil {
		return userErrorf("Invalid rank: use a whole number, e.g. 10, 20, 30")
	}
	_, err = s.store.CreateLevel(r.Context(), domain.Level{
		Track: domain.Track(formStr(r, "track")), Code: formStr(r, "code"), Title: formStr(r, "title"), Rank: rank,
	})
	if err != nil {
		return err
	}
	return s.done(w, r, "/settings", "Level added.")
}

func (s *Server) settingsMeetingKind(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.store.CreateMeetingKind(r.Context(), formStr(r, "label"), formStr(r, "requires_person") == "1"); err != nil {
		return err
	}
	return s.done(w, r, "/settings", "Meeting kind added.")
}

func (s *Server) settingsEntryKind(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.store.CreateEntryKind(r.Context(), formStr(r, "label"), formStr(r, "requires_outcome") == "1"); err != nil {
		return err
	}
	return s.done(w, r, "/settings", "Entry kind added.")
}

func (s *Server) settingsPrompt(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.store.CreatePrompt(r.Context(), formStr(r, "meeting_kind"), formStr(r, "text")); err != nil {
		return err
	}
	return s.done(w, r, "/settings", "Question added to the minutes template.")
}
