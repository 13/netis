package web

import (
	"net/http"
	"regexp"

	"netis/internal/web/views"
)

// colorTokenRe matches a colour token in app.css, written once for both
// themes as --name:light-dark(#light, #dark).
var colorTokenRe = regexp.MustCompile(`--([a-z0-9-]+):light-dark\((#[0-9A-Fa-f]{6}),\s*(#[0-9A-Fa-f]{6})\)`)

// colorTokens reads the palette out of the embedded app.css, in the order it
// is written, so the style guide shows exactly what the pages use.
func colorTokens() []views.ColorToken {
	b, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		return nil
	}
	var out []views.ColorToken
	for _, m := range colorTokenRe.FindAllStringSubmatch(string(b), -1) {
		out = append(out, views.ColorToken{Name: m[1], Light: m[2], Dark: m[3]})
	}
	return out
}

// handleStyleguide serves the living style tile: palette, type, icons and
// every component, in the viewer's current theme.
func (s *Server) handleStyleguide(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	s.render(w, r, views.StyleguidePage(u.Username, colorTokens()))
}
