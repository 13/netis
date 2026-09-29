// Package autofill fills in device details (vendor, model, kind, icon,
// function, name, tags) from hints that sources observe, without ever
// overwriting what a person set. See
// docs/superpowers/specs/2026-09-29-netis-autofill-design.md.
package autofill

import (
	"regexp"
	"sort"
	"strings"

	"netis/internal/store"
)

// Threshold is the lowest confidence that is applied. Lower hints are kept
// and shown as suggestions only.
const Threshold = 50

// Field names, matching device columns (and "tag").
const (
	FieldVendor   = "vendor"
	FieldModel    = "model"
	FieldKind     = "kind"
	FieldIcon     = "icon"
	FieldFunction = "function"
	FieldName     = "name"
	FieldTag      = "tag"
)

// singleFields are the one-value fields, in the order changes are listed.
var singleFields = []string{FieldVendor, FieldModel, FieldKind, FieldIcon, FieldFunction, FieldName}

// sourceRank breaks confidence ties: a source that looked closer wins.
var sourceRank = map[string]int{"mdns": 5, "ssdp": 4, "ports": 3, "hostname": 2, "oui": 1}

// Candidate is the value a field would get, and why.
type Candidate struct {
	Value, Source, Detail string
	Confidence            int
}

func better(a store.Hint, b Candidate) bool {
	if a.Confidence != b.Confidence {
		return a.Confidence > b.Confidence
	}
	if ra, rb := sourceRank[a.Source], sourceRank[b.Source]; ra != rb {
		return ra > rb
	}
	return a.Value < b.Value
}

// Resolve picks the winning candidate per single-valued field and the tags
// to add, ignoring hints below Threshold. Tags are sorted by name.
func Resolve(hints []store.Hint) (map[string]Candidate, []Candidate) {
	fields := map[string]Candidate{}
	tagBy := map[string]Candidate{}
	for _, h := range hints {
		if h.Confidence < Threshold || h.Value == "" {
			continue
		}
		c := Candidate{Value: h.Value, Source: h.Source, Detail: h.Detail, Confidence: h.Confidence}
		if h.Field == FieldTag {
			if cur, ok := tagBy[h.Value]; !ok || better(h, cur) {
				tagBy[h.Value] = c
			}
			continue
		}
		if cur, ok := fields[h.Field]; !ok || better(h, cur) {
			fields[h.Field] = c
		}
	}
	tags := make([]Candidate, 0, len(tagBy))
	for _, c := range tagBy {
		tags = append(tags, c)
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Value < tags[j].Value })
	return fields, tags
}

// placeholderRe matches the names netis makes up for a device it knows
// nothing about: unknown-<mac|ip> and private-<mac> from a sweep,
// <source>-<mac> from a lease sync.
var placeholderRe = regexp.MustCompile(`^(unknown|private|pihole|adguard|opnsense)-[0-9a-fA-F:.]+$`)

// IsPlaceholderName reports whether name is one netis made up.
func IsPlaceholderName(name string) bool { return placeholderRe.MatchString(name) }

func current(d store.Device, field string) string {
	switch field {
	case FieldVendor:
		return d.Vendor
	case FieldModel:
		return d.Model
	case FieldKind:
		return d.Kind
	case FieldIcon:
		return d.Icon
	case FieldFunction:
		return d.Function
	case FieldName:
		return d.Name
	}
	return ""
}

// isEmpty reports whether a field holds nothing a person would miss.
func isEmpty(d store.Device, field, cur string) bool {
	switch field {
	case FieldKind:
		return cur == "" || cur == "other"
	case FieldName:
		return !d.Reviewed && IsPlaceholderName(cur)
	}
	return cur == ""
}

func recordsByField(recs []store.AutofillRecord) map[string]store.AutofillRecord {
	m := make(map[string]store.AutofillRecord, len(recs))
	for _, r := range recs {
		m[r.Field] = r
	}
	return m
}

// decide applies the ownership rules to one device. It is pure: the store
// re-checks each write against the value read here (AutofillWrite.Expect).
func decide(d store.Device, tags []string, recs []store.AutofillRecord, fields map[string]Candidate, tagCands []Candidate) store.AutofillChanges {
	by := recordsByField(recs)
	var ch store.AutofillChanges
	for _, f := range singleFields {
		cur := current(d, f)
		rec, has := by[f]
		if has && rec.State == store.AutofillOwned {
			continue
		}
		if has && cur != rec.Value {
			ch.Own = append(ch.Own, f)
			continue
		}
		c, ok := fields[f]
		if !ok {
			continue
		}
		switch {
		case isEmpty(d, f, cur) && c.Value != cur:
			ch.Writes = append(ch.Writes, store.AutofillWrite{Field: f, Value: c.Value, Source: c.Source,
				Expect: cur, Unreviewed: f == FieldName})
		case has && !d.Reviewed && c.Value != cur:
			ch.Writes = append(ch.Writes, store.AutofillWrite{Field: f, Value: c.Value, Source: c.Source,
				Expect: cur, Unreviewed: true})
		}
	}

	have := make(map[string]bool, len(tags))
	for _, t := range tags {
		have[t] = true
	}
	for _, r := range recs {
		if name, ok := strings.CutPrefix(r.Field, "tag:"); ok && r.State == store.AutofillApplied && !have[name] {
			ch.Own = append(ch.Own, r.Field)
		}
	}
	for _, c := range tagCands {
		if _, has := by["tag:"+c.Value]; has || have[c.Value] {
			continue
		}
		ch.Tags = append(ch.Tags, store.AutofillTag{Name: c.Value, Source: c.Source})
	}
	return ch
}

// Hint statuses, for the device page.
const (
	StatusApplied   = "applied"   // the device shows this value because of this hint
	StatusLow       = "low"       // below Threshold: a suggestion only
	StatusOwned     = "owned"     // a person set this field; autofill keeps out
	StatusOutranked = "outranked" // another hint for the field won
	StatusKept      = "kept"      // the field already had a value autofill did not write
)

// Explained is a hint with what became of it.
type Explained struct {
	store.Hint
	Status string
}

// Explain says, for every hint, why the device does or does not show it.
func Explain(d store.Device, tags []string, recs []store.AutofillRecord, hints []store.Hint) []Explained {
	by := recordsByField(recs)
	fields, _ := Resolve(hints)
	have := make(map[string]bool, len(tags))
	for _, t := range tags {
		have[t] = true
	}
	out := make([]Explained, 0, len(hints))
	for _, h := range hints {
		key := h.Field
		cur := current(d, h.Field)
		if h.Field == FieldTag {
			key = "tag:" + h.Value
		}
		rec, has := by[key]
		var st string
		switch {
		case has && rec.State == store.AutofillOwned:
			st = StatusOwned
		// A person cleared or changed the value the record applied, but no
		// pass has run since to mark the record owned: treat it as theirs
		// now, not as a hint autofill still owns.
		case h.Field != FieldTag && has && rec.State == store.AutofillApplied && cur != rec.Value:
			st = StatusOwned
		case h.Field == FieldTag && has && rec.State == store.AutofillApplied && !have[h.Value]:
			st = StatusOwned
		case h.Confidence < Threshold:
			st = StatusLow
		case h.Field == FieldTag && has && have[h.Value]:
			st = StatusApplied
		case h.Field == FieldTag:
			st = StatusKept
		case has && cur == rec.Value && cur == h.Value:
			st = StatusApplied
		case fields[h.Field].Source != h.Source || fields[h.Field].Value != h.Value:
			st = StatusOutranked
		default:
			st = StatusKept
		}
		out = append(out, Explained{Hint: h, Status: st})
	}
	return out
}
