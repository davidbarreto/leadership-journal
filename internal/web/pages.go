package web

import "net/http"

// registerPages adds the journal's own pages to the router.
func (s *Server) registerPages(rt router) {
	rt.protected("GET /people", s.dashboard) // placeholder until the people pages exist
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) error {
	drafts, err := s.store.PendingDrafts(r.Context())
	if err != nil {
		return err
	}
	return s.render(w, r, http.StatusOK, "dashboard", "dashboard", struct{ Drafts any }{drafts})
}
