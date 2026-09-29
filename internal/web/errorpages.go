package web

import (
	"net/http"
	"strings"

	"netis/internal/web/views"
)

// pageErrors are the statuses a browser gets a page for instead of the
// one-line text http.Error writes. A 400 keeps its text: it carries the
// specific thing the form got wrong.
var pageErrors = map[int]bool{
	http.StatusForbidden:           true,
	http.StatusNotFound:            true,
	http.StatusInternalServerError: true,
	http.StatusServiceUnavailable:  true,
}

// wantsPage reports whether r is a browser loading a page, the only kind of
// request an error page is for. Scripts on /api/ and /metrics keep their
// JSON or text, and an htmx request keeps the short text it can show in
// place.
func wantsPage(r *http.Request) bool {
	return !forMachines(r.URL.Path) && r.Header.Get("HX-Request") == "" &&
		strings.Contains(r.Header.Get("Accept"), "text/html")
}

// errorPages answers a browser's page load that ended in one of the
// pageErrors, written with http.Error (the mux's own 404 for an unknown
// address included), with an error page in the app's frame: what happened in
// plain words and a way back. On /api/ every plain-text error becomes the
// API's JSON error instead, so a script gets JSON even from an address that
// does not exist. Handlers keep calling http.Error; only what reaches the
// client changes.
func (s *Server) errorPages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api := strings.HasPrefix(r.URL.Path, "/api/")
		if !api && !wantsPage(r) {
			next.ServeHTTP(w, r)
			return
		}
		ew := &errorPageWriter{ResponseWriter: w, anyError: api}
		next.ServeHTTP(ew, r)
		switch {
		case ew.code == 0:
		case api:
			msg := strings.TrimSpace(ew.body.String())
			if ew.code == http.StatusNotFound && msg == "404 page not found" {
				msg = "not found"
			}
			w.Header().Del("X-Content-Type-Options")
			w.Header().Del("Content-Length")
			s.apiError(w, r, ew.code, msg)
		default:
			s.errorPage(w, r, ew.code)
		}
	})
}

// errorPageWriter holds back a plain-text error response, and keeps its
// text, so errorPages can answer with a page or JSON instead. Anything else
// passes straight through.
type errorPageWriter struct {
	http.ResponseWriter
	// anyError holds back every error status, not just the pageErrors.
	anyError bool
	code     int  // the held-back status, 0 when none
	started  bool // a status line has gone out (or been held back)
	body     strings.Builder
}

func (e *errorPageWriter) WriteHeader(code int) {
	held := pageErrors[code] || (e.anyError && code >= 400)
	if !e.started && held && strings.HasPrefix(e.Header().Get("Content-Type"), "text/plain") {
		e.code, e.started = code, true
		return
	}
	e.started = true
	e.ResponseWriter.WriteHeader(code)
}

func (e *errorPageWriter) Write(b []byte) (int, error) {
	if e.code != 0 {
		// An error message is one line; keep no more than a short one.
		if e.body.Len() < 512 {
			e.body.Write(b)
		}
		return len(b), nil
	}
	e.started = true
	return e.ResponseWriter.Write(b)
}

func (e *errorPageWriter) Unwrap() http.ResponseWriter { return e.ResponseWriter }

// errorPage writes the error page for code: in the app shell for a signed-in
// user, in the minimal shell otherwise.
func (s *Server) errorPage(w http.ResponseWriter, r *http.Request, code int) {
	d := views.ErrorPageData{Code: code}
	if u, ok := userFrom(r); ok {
		d.SignedIn, d.Username = true, u.Username
	}
	h := w.Header()
	h.Del("Content-Length")
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	s.render(w, r, views.ErrorPage(d))
}
