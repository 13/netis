package web

import (
	"net/http"
	"net/url"
	"strings"
)

// toastCookie carries a message from a form post to the page it redirects
// to, where toasts.js shows it once and clears it.
const toastCookie = "netis_toast"

// flashToast leaves msg for the next page to show as a toast, so a plain
// form post that redirects still tells the user it worked ("Link added").
// The cookie is readable by script, on purpose: it holds only the message,
// which toasts.js inserts as text.
func (s *Server) flashToast(w http.ResponseWriter, r *http.Request, msg string) {
	http.SetCookie(w, &http.Cookie{
		Name:     toastCookie,
		Value:    strings.ReplaceAll(url.QueryEscape(msg), "+", "%20"),
		Path:     "/",
		MaxAge:   60,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureRequest(r),
	})
}
