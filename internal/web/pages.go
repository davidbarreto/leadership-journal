package web

// registerPages adds the journal's own pages to the router.
func (s *Server) registerPages(rt router) {
	s.registerPeople(rt)
	s.registerMeetings(rt)
	s.registerEntries(rt)
	s.registerCommitments(rt)
	s.registerWork(rt)
	s.registerSearch(rt)
	s.registerSettings(rt)
	s.registerAccount(rt)
	s.registerReport(rt)
}
