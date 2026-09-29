package views

import "strconv"

// occSegment is one coloured run of the dashboard occupancy bar, in percent
// of its width.
type occSegment struct {
	Class string
	X, W  float64
}

// occSegments lays the online, not-seen-yet and offline runs of a subnet's
// occupancy bar end to end. The bar is an SVG, so the widths are attributes
// rather than inline styles.
func occSegments(r DashRow) []occSegment {
	if r.Hosts <= 0 {
		return nil
	}
	var out []occSegment
	x := 0.0
	for _, s := range []struct {
		class string
		n     int
	}{{"on", r.Online}, {"res", r.Unseen}, {"off", r.Offline}} {
		w := float64(s.n) * 100 / float64(r.Hosts)
		if w > 0 {
			out = append(out, occSegment{Class: s.class, X: x, W: w})
		}
		x += w
	}
	return out
}

// pct formats a percentage for an SVG attribute.
func pct(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64) + "%"
}
