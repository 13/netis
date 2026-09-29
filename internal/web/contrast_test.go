package web

import (
	"math"
	"strconv"
	"testing"
)

// cssTokens returns the light and dark hex value of each colour token in
// app.css, by name.
func cssTokens(t *testing.T) (light, dark map[string]string) {
	t.Helper()
	light, dark = map[string]string{}, map[string]string{}
	for _, c := range colorTokens() {
		light[c.Name], dark[c.Name] = c.Light, c.Dark
	}
	if len(light) < 16 {
		t.Fatalf("found only %d colour tokens in app.css; the parse is broken", len(light))
	}
	return light, dark
}

// luminance is the WCAG relative luminance of a #rrggbb colour.
func luminance(hex string) float64 {
	ch := func(i int) float64 {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(0) + 0.7152*ch(1) + 0.0722*ch(2)
}

func contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Every text colour meets WCAG AA (4.5:1) on every background it is drawn on,
// and every colour that carries meaning without words (status LEDs, control
// borders, the focus ring) meets 3:1, in both themes.
func TestTokenContrast(t *testing.T) {
	light, dark := cssTokens(t)
	surfaces := []string{"bg", "surface", "surface-2"}
	type pair struct {
		fg, bg string
		min    float64
	}
	var pairs []pair
	for _, bg := range surfaces {
		for _, fg := range []string{"fg", "muted", "accent", "fault"} {
			pairs = append(pairs, pair{fg, bg, 4.5}) // text, links, danger text
		}
		for _, fg := range []string{"link", "activity", "fault", "border-strong", "accent"} {
			pairs = append(pairs, pair{fg, bg, 3}) // LEDs, control borders, focus ring
		}
	}
	for _, soft := range []string{"link-soft", "activity-soft", "fault-soft", "accent-soft"} {
		pairs = append(pairs, pair{"fg", soft, 4.5}) // status labels, error box
		pairs = append(pairs, pair{"muted", soft, 4.5})
	}
	pairs = append(pairs,
		pair{"accent-fg", "accent", 4.5},  // primary button
		pair{"accent", "accent-soft", 4.5}, // selected icon swatch
		pair{"fault", "fault-soft", 4.5},   // danger button hover
	)
	for theme, tok := range map[string]map[string]string{"light": light, "dark": dark} {
		for _, p := range pairs {
			fg, ok1 := tok[p.fg]
			bg, ok2 := tok[p.bg]
			if !ok1 || !ok2 {
				t.Errorf("token --%s or --%s missing", p.fg, p.bg)
				continue
			}
			if c := contrast(fg, bg); c < p.min {
				t.Errorf("%s: --%s %s on --%s %s is %.2f:1, want %.1f:1", theme, p.fg, fg, p.bg, bg, c, p.min)
			}
		}
	}
}
