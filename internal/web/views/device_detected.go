package views

import (
	"netis/internal/autofill"
	"netis/internal/store"
)

// sourceLabels names hint sources for people.
var sourceLabels = map[string]string{
	"oui": "the MAC vendor list", "hostname": "the hostname", "ports": "open ports",
	"mdns": "mDNS", "ssdp": "UPnP",
}

func sourceLabel(src string) string {
	if l, ok := sourceLabels[src]; ok {
		return l
	}
	return src
}

// detectedFrom returns the badge title for a field autofill filled and the
// device still shows, or "" when a person set it.
func (d DeviceDetail) detectedFrom(field string) string {
	rec, ok := d.Autofilled[field]
	if !ok || rec.Value != fieldValue(d.Device, field) {
		return ""
	}
	return "Detected from " + sourceLabel(rec.Source)
}

func fieldValue(dev store.Device, field string) string {
	switch field {
	case "vendor":
		return dev.Vendor
	case "model":
		return dev.Model
	case "kind":
		return dev.Kind
	case "icon":
		return dev.Icon
	case "function":
		return dev.Function
	case "name":
		return dev.Name
	}
	return ""
}

// hintStatus says what became of a hint. A kept hint is "already set" when
// the field already holds a value autofill did not write (or the device
// already has the tag), and "not used yet" when it is empty (the next pass
// will fill it).
func (d DeviceDetail) hintStatus(e autofill.Explained) string {
	if e.Status == autofill.StatusKept && d.alreadySet(e) {
		return "Not used: already set"
	}
	return statusText[e.Status]
}

func (d DeviceDetail) alreadySet(e autofill.Explained) bool {
	if e.Field == autofill.FieldTag {
		for _, t := range d.Tags {
			if t.Name == e.Value {
				return true
			}
		}
		return false
	}
	cur := fieldValue(d.Device, e.Field)
	return cur != "" && !(e.Field == "kind" && cur == "other")
}

var statusText = map[string]string{
	autofill.StatusApplied:   "Used",
	autofill.StatusLow:       "Suggestion",
	autofill.StatusOwned:     "Not used: you set this",
	autofill.StatusOutranked: "Not used: another clue won",
	autofill.StatusKept:      "Not used yet",
}

var fieldText = map[string]string{
	"vendor": "Vendor", "model": "Model", "kind": "Kind", "icon": "Icon",
	"function": "Function", "name": "Name", "tag": "Tag",
}

// hintValue is a hint's value as the page shows it elsewhere: a kind in
// words, anything else as is.
func hintValue(e autofill.Explained) string {
	if e.Field == "kind" {
		return kindName(e.Value)
	}
	return e.Value
}
