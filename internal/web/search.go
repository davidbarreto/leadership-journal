package web

import (
	"fmt"
	"net/http"
	"strings"

	"leadership-journal/internal/store"
)

func (s *Server) registerSearch(rt router) {
	rt.protected("GET /search", s.search)
}

type hitView struct {
	store.SearchHit
	Label string
	Link  string
}

type searchPage struct {
	Query    string
	Searched bool
	Hits     []hitView
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) error {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page := searchPage{Query: q, Searched: q != ""}
	if page.Searched {
		hits, err := s.store.Search(r.Context(), q, 50)
		if err != nil {
			return err
		}
		for _, h := range hits {
			v := hitView{SearchHit: h}
			switch h.Kind {
			case "entry":
				v.Label = "Note"
				if h.PersonID != nil {
					v.Link = fmt.Sprintf("/people/%d", *h.PersonID)
				}
			case "meeting":
				v.Label = "Meeting"
			default:
				v.Label = "Meeting answer"
			}
			if h.Kind != "entry" && h.MeetingID != nil {
				v.Link = fmt.Sprintf("/meetings/%d", *h.MeetingID)
			}
			page.Hits = append(page.Hits, v)
		}
		s.log.Info("search", "results", len(hits), "request_id", requestID(r.Context())) // never the query: it may name a person
	}
	return s.render(w, r, http.StatusOK, "search", "search", page)
}
