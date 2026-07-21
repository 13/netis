package web

import (
	"net/http"

	"netis/internal/web/views"
)

func (s *Server) handleEventsPage(w http.ResponseWriter, r *http.Request) {
	evs, err := s.store.ListEvents(r.Context(), 200)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	typeFilter := r.URL.Query().Get("type")
	if typeFilter != "" {
		filtered := evs[:0]
		for _, e := range evs {
			if e.Type == typeFilter {
				filtered = append(filtered, e)
			}
		}
		evs = filtered
	}
	u, _ := userFrom(r)
	views.EventsPage(u.Username, evs, typeFilter).Render(r.Context(), w)
}
