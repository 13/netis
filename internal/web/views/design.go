package views

import "strconv"

// eventClass is the label style for an event type: a status in the colour of
// what the event says about the device, or a plain status with a hollow dot
// for events that are news but not a state (an IP change). A type added later
// gets the plain style rather than none.
func eventClass(eventType string) string {
	switch eventType {
	case "online", "sync_recovered", "device_returned":
		return "status is-online"
	case "offline":
		return "status is-offline"
	case "device_new":
		return "status is-new"
	case "device_missing":
		return "status is-missing"
	case "ip_conflict":
		return "status is-conflict"
	case "scan_error":
		return "status is-error"
	}
	return "status"
}

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
