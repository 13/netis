package views

import (
	"fmt"
	"strings"
	"time"

	"netis/internal/macaddr"
	"netis/internal/store"
)

// BackLink is where the device page's back link goes: the page the user
// came from when it is one of ours, the device list otherwise.
type BackLink struct {
	URL, Label string
}

// AvailDay is one day of the availability bar.
type AvailDay struct {
	Date    time.Time
	Pct     float64 // share of the day's swept hours the device was up, 0-100
	HasData bool
}

// Availability is the device's availability over the bar's days, oldest
// first, with the figure for the whole span and its worst day.
type Availability struct {
	Days    []AvailDay
	Pct     float64
	HasData bool
	Low     AvailDay // the day with the lowest figure, when HasData
}

// AvailabilityDays is the span the device page's availability bar covers.
const AvailabilityDays = 30

// BuildAvailability folds hourly buckets into one entry per day for the
// days ending with now's day, in now's time zone. A day's figure is the mean
// of its swept hours; a day with none has no data, which is not the same as
// being down, so it neither counts against the total nor becomes the low.
func BuildAvailability(hours []store.AvailabilityBucket, now time.Time, days int) Availability {
	loc := now.Location()
	y, m, d := now.Date()
	first := time.Date(y, m, d-days+1, 0, 0, 0, 0, loc)
	sums := make([]float64, days)
	counts := make([]int, days)
	var total float64
	var n int
	for _, h := range hours {
		t, err := time.Parse(time.RFC3339, h.Start)
		if err != nil {
			continue
		}
		t = t.In(loc)
		ty, tm, td := t.Date()
		// Count calendar days rather than dividing durations, so a day
		// that is 23 or 25 hours long across a DST change stays one day.
		i := int(time.Date(ty, tm, td, 12, 0, 0, 0, time.UTC).Sub(time.Date(first.Year(), first.Month(), first.Day(), 12, 0, 0, 0, time.UTC)).Hours() / 24)
		if i < 0 || i >= days {
			continue
		}
		sums[i] += h.Up
		counts[i]++
		total += h.Up
		n++
	}
	a := Availability{Days: make([]AvailDay, days)}
	for i := range a.Days {
		day := AvailDay{Date: time.Date(first.Year(), first.Month(), first.Day()+i, 0, 0, 0, 0, loc)}
		if counts[i] > 0 {
			day.HasData = true
			day.Pct = 100 * sums[i] / float64(counts[i])
			if !a.HasData || day.Pct < a.Low.Pct {
				a.Low = day
			}
			a.HasData = true
		}
		a.Days[i] = day
	}
	if n > 0 {
		a.Pct = 100 * total / float64(n)
	}
	return a
}

// fmtPct renders an availability figure: one decimal, but never "100.0%"
// for a device that was down at all, nor a trailing ".0" when it was not.
func fmtPct(p float64) string {
	switch {
	case p >= 100:
		return "100%"
	case p > 99.9:
		return "99.9%"
	}
	return fmt.Sprintf("%.1f%%", p)
}

// availSummary is the bar's text: what the figure is and the worst day, for
// the eye and for a screen reader alike.
func availSummary(a Availability) string {
	if !a.HasData {
		return "No availability recorded in the last 30 days yet."
	}
	s := fmtPct(a.Pct) + " online over the last 30 days."
	if a.Low.Pct < 99.95 {
		s += " Lowest day: " + a.Low.Date.Format("2 Jan") + " at " + fmtPct(a.Low.Pct) + "."
	}
	return s
}

// availBarTitle is one day's hover text.
func availBarTitle(d AvailDay) string {
	if !d.HasData {
		return d.Date.Format("Mon 2 Jan") + ": no data"
	}
	return d.Date.Format("Mon 2 Jan") + ": " + fmtPct(d.Pct) + " online"
}

// availBar is one day's geometry in the bar's 0-300 by 0-40 view box: a
// full-height track and, from the bottom, the online share.
type availBar struct {
	X, FillY, FillH string
	Tone            string
	Day             AvailDay
}

// availBars lays the days out left to right, oldest first.
func availBars(a Availability) []availBar {
	const w, h = 10.0, 40.0
	bars := make([]availBar, len(a.Days))
	for i, d := range a.Days {
		fh := h * d.Pct / 100
		tone := "up"
		switch {
		case !d.HasData:
			tone = "none"
		case d.Pct < 90:
			tone = "low"
		case d.Pct < 99.5:
			tone = "part"
		}
		if d.HasData && fh < 2 {
			fh = 2 // a day that was down still shows a sliver of track floor
		}
		bars[i] = availBar{
			X:     fmt.Sprintf("%.1f", float64(i)*w+1),
			FillY: fmt.Sprintf("%.2f", h-fh),
			FillH: fmt.Sprintf("%.2f", fh),
			Tone:  tone,
			Day:   d,
		}
	}
	return bars
}

// kindName is a device kind in words, for people rather than the schema.
func kindName(kind string) string {
	switch kind {
	case "wg-peer":
		return "WireGuard peer"
	case "vm":
		return "Virtual machine"
	case "lxc":
		return "LXC container"
	case "iot":
		return "IoT device"
	case "other", "":
		return "Other"
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}

// primaryIP is the address shown under the device's name: the first IP of
// its first interface that has one.
func (d DeviceDetail) primaryIP() string {
	for _, f := range d.Ifaces {
		if len(f.IPs) > 0 {
			return f.IPs[0].IP
		}
	}
	return ""
}

// hasPrivateMAC says whether any interface has a randomized MAC, which the
// header flags since the device may come back under another address.
func (d DeviceDetail) hasPrivateMAC() bool {
	for _, f := range d.Ifaces {
		if f.Iface.MAC != nil && macaddr.IsPrivate(*f.Iface.MAC) {
			return true
		}
	}
	return false
}

// proxmoxStatus is a guest's state as Proxmox last reported it, or "".
func (d DeviceDetail) proxmoxStatus() string {
	if d.Device.Kind != "vm" && d.Device.Kind != "lxc" {
		return ""
	}
	for _, f := range d.Fields {
		if f.Key == "proxmox_status" {
			return f.Value
		}
	}
	return ""
}

// subnetOf names the subnet an IP belongs to, for its link.
func (d DeviceDetail) subnetOf(id int64) (store.Subnet, bool) {
	sn, ok := d.Subnets[id]
	return sn, ok
}

// relatedIP is the lowest IP of a parent or child row, or "".
func relatedIP(r store.DeviceRow) string { return lowestIPStr(r.IPs) }

// DeviceEventLimit is how many of its latest events the device page lists.
const DeviceEventLimit = 20
