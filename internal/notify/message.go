package notify

import (
	"fmt"
	"strings"
	"time"
)

// Item is one event accepted for sending.
type Item struct {
	Type       string
	DeviceID   *int64
	DeviceName string
	Details    string
	Time       time.Time
}

// Message is what goes out on every channel for one batch of items.
type Message struct {
	Event    string // the event type, "batch" or "test"
	Title    string
	Body     string
	Priority int // ntfy priority, 1-5
	Tags     []string
	URL      string
	Time     time.Time
	Items    []Item
}

// maxLines bounds the event lines listed in a summary: a scan that finds a
// whole new subnet should produce a readable message, not a wall.
const maxLines = 20

type typeInfo struct {
	title    string
	tag      string // ntfy tag, rendered as an emoji
	priority int
	noun     string // for the summary line, singular and plural
	nouns    string
}

var types = map[string]typeInfo{
	"device_new":     {"New device", "new", 3, "new device", "new devices"},
	"offline":        {"Device offline", "red_circle", 4, "went offline", "went offline"},
	"online":         {"Device online", "green_circle", 3, "came online", "came online"},
	"ip_conflict":    {"IP conflict", "warning", 4, "IP conflict", "IP conflicts"},
	"scan_error":     {"Scan or sync failing", "rotating_light", 4, "error", "errors"},
	"sync_recovered": {"Integration recovered", "white_check_mark", 2, "recovery", "recoveries"},
}

// summaryOrder is the order counts appear in a batch's summary line.
var summaryOrder = []string{"device_new", "offline", "online", "ip_conflict", "scan_error", "sync_recovered"}

// buildMessage turns a batch into one message: a single event is sent as
// itself, several as a summary with one line each.
func buildMessage(c Config, items []Item) Message {
	if len(items) == 1 {
		it := items[0]
		ti := types[it.Type]
		m := Message{
			Event: it.Type, Title: "netis: " + ti.title, Body: it.Details,
			Priority: ti.priority, Tags: []string{ti.tag}, Time: it.Time, Items: items,
		}
		if it.DeviceID != nil {
			m.URL = c.link(fmt.Sprintf("/devices/%d", *it.DeviceID))
		} else {
			m.URL = c.link("/events")
		}
		return m
	}

	counts := map[string]int{}
	for _, it := range items {
		counts[it.Type]++
	}
	var parts, tags []string
	prio := 1
	for _, typ := range summaryOrder {
		n := counts[typ]
		if n == 0 {
			continue
		}
		ti := types[typ]
		noun := ti.nouns
		if n == 1 {
			noun = ti.noun
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, noun))
		tags = append(tags, ti.tag)
		prio = max(prio, ti.priority)
	}
	summary := strings.Join(parts, ", ")
	var b strings.Builder
	b.WriteString(summary)
	for i, it := range items {
		if i == maxLines {
			fmt.Fprintf(&b, "\n… and %d more", len(items)-maxLines)
			break
		}
		b.WriteString("\n- " + it.Details)
	}
	return Message{
		Event: "batch", Title: fmt.Sprintf("netis: %d events", len(items)), Body: b.String(),
		Priority: prio, Tags: tags, URL: c.link("/events"), Time: items[len(items)-1].Time,
		Items: items,
	}
}

// summaryLine is the first line of a batch body.
func (m Message) summaryLine() string {
	line, _, _ := strings.Cut(m.Body, "\n")
	return line
}

func testMessage(c Config) Message {
	return Message{
		Event: "test", Title: "netis: test notification",
		Body:     "Notifications from netis are working.",
		Priority: 3, Tags: []string{"white_check_mark"}, URL: c.link("/"),
		Time: time.Now().UTC(),
	}
}
