package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"netis/internal/web/views"
)

// eventsPageSize is how many events one page of the log shows.
const eventsPageSize = 50

// eventDayBound is the UTC timestamp at which the YYYY-MM-DD day starts in
// the server's zone, shifted by days; "" for a value that is not a date.
func eventDayBound(v string, days int) string {
	t, err := time.ParseInLocation("2006-01-02", v, time.Local)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, days).UTC().Format(time.RFC3339)
}

func (s *Server) handleEventsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	u, _ := userFrom(r)
	d := views.EventsPageData{
		Username: u.Username,
		Kind:     q.Get("type"),
		Device:   strings.TrimSpace(q.Get("device")),
		From:     q.Get("from"),
		To:       q.Get("to"),
	}
	f, ok := views.EventKindFilter(d.Kind)
	if !ok {
		// An unknown kind filters nothing rather than failing the page.
		d.Kind = ""
	}
	f.Device = d.Device
	if f.Since = eventDayBound(d.From, 0); f.Since == "" {
		d.From = ""
	}
	// To is inclusive: the range ends where the next day starts.
	if f.Until = eventDayBound(d.To, 1); f.Until == "" {
		d.To = ""
	}
	if n, err := strconv.ParseInt(q.Get("before"), 10, 64); err == nil && n > 0 {
		f.BeforeID = n
		d.BeforeID = n
	}
	f.Limit = eventsPageSize

	evs, more, err := s.store.ListEventsFiltered(r.Context(), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if more && len(evs) > 0 {
		d.Older = evs[len(evs)-1].ID
	}
	d.Days = views.GroupEventsByDay(evs, time.Now())
	s.render(w, r, views.EventsPage(d))
}
