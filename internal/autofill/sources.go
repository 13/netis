package autofill

import (
	"fmt"
	"regexp"
	"strings"

	"netis/internal/oui"
	"netis/internal/store"
)

func hint(field, value string, conf int) store.Hint {
	return store.Hint{Field: field, Value: value, Confidence: conf}
}

// vendorKinds are makers that make essentially one kind of device. The hint
// is below Threshold: a suggestion, not a fill.
var vendorKinds = map[string]string{
	"Espressif":  "iot",
	"Tuya Smart": "iot",
	"Shelly":     "iot",
	"Brother":    "printer",
	"Epson":      "printer",
}

// ouiHints names each MAC's maker.
func ouiHints(macs []string) []store.Hint {
	var out []store.Hint
	for _, mac := range macs {
		v := oui.Vendor(mac)
		if v == "" {
			continue
		}
		detail := "MAC " + mac
		h := hint(FieldVendor, v, 90)
		h.Detail = detail
		out = append(out, h)
		if k, ok := vendorKinds[v]; ok {
			h := hint(FieldKind, k, 40)
			h.Detail = detail
			out = append(out, h)
		}
	}
	return dedupe(out)
}

// named is a name the hostname rules look at, and what it is ("hostname" or
// "name"), for the evidence shown to people.
type named struct{ Label, Value string }

type hostRule struct {
	re    *regexp.Regexp
	hints []store.Hint
}

func rule(pattern string, hints ...store.Hint) hostRule {
	return hostRule{re: regexp.MustCompile(pattern), hints: hints}
}

// sep matches the start or end of a word inside a hostname label.
const (
	pre  = `(^|[-_.])`
	post = `([-_.0-9]|$)`
)

// hostRules match the first label of a hostname, lower-cased. Each rule's
// patterns are exercised in sources_test.go.
var hostRules = []hostRule{
	rule(`iphone`, hint(FieldKind, "phone", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "iPhone", 60)),
	rule(`ipad`, hint(FieldKind, "phone", 60), hint(FieldIcon, "tablet", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "iPad", 60)),
	rule(`macbook-?pro`, hint(FieldKind, "computer", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "MacBook Pro", 65)),
	rule(`macbook-?air`, hint(FieldKind, "computer", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "MacBook Air", 65)),
	rule(`macbook`, hint(FieldKind, "computer", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "MacBook", 60)),
	rule(`imac`, hint(FieldKind, "computer", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "iMac", 60)),
	rule(`mac-?mini`, hint(FieldKind, "computer", 70), hint(FieldVendor, "Apple", 70), hint(FieldModel, "Mac mini", 60)),
	rule(`galaxy|^sm-[a-z]\d`, hint(FieldKind, "phone", 70), hint(FieldVendor, "Samsung", 70)),
	rule(`^pixel`, hint(FieldKind, "phone", 70), hint(FieldVendor, "Google", 70)),
	rule(`^android-`, hint(FieldKind, "phone", 60)),
	rule(`^(desktop|laptop)-[a-z0-9]{7}$`, hint(FieldKind, "computer", 70)),
	rule(`^br[wn][0-9a-f]{12}$`, hint(FieldKind, "printer", 80), hint(FieldVendor, "Brother", 80)),
	rule(`^(hp|npi)[0-9a-f]{6}`, hint(FieldKind, "printer", 80), hint(FieldVendor, "HP", 80)),
	rule(`epson`, hint(FieldKind, "printer", 70), hint(FieldVendor, "Epson", 70)),
	rule(`^esp[-_]|tasmota|shelly|esphome`, hint(FieldKind, "iot", 70), hint(FieldTag, "smart-home", 60)),
	rule(`chromecast|roku|apple-?tv|fire-?tv`, hint(FieldIcon, "tv", 70), hint(FieldTag, "media", 60)),
	rule(`sonos`, hint(FieldIcon, "speaker", 70), hint(FieldVendor, "Sonos", 70), hint(FieldTag, "media", 60)),
	rule(pre+`ps[45]`+post+`|xbox|nintendo`, hint(FieldIcon, "gamepad-2", 70)),
	rule(`raspberrypi`, hint(FieldKind, "computer", 60), hint(FieldVendor, "Raspberry Pi", 60)),
	rule(`synology|diskstation|truenas|`+pre+`nas`+post, hint(FieldKind, "server", 60), hint(FieldTag, "nas", 60)),
	rule(`^pve`+post+`|proxmox`, hint(FieldKind, "server", 70), hint(FieldFunction, "Proxmox VE", 60)),
}

// hostnameHints runs the rules over each name's first label. Placeholder
// names netis made up are skipped.
func hostnameHints(names []named) []store.Hint {
	var out []store.Hint
	for _, n := range names {
		if n.Value == "" || IsPlaceholderName(n.Value) {
			continue
		}
		label, _, _ := strings.Cut(strings.ToLower(n.Value), ".")
		for _, r := range hostRules {
			if !r.re.MatchString(label) {
				continue
			}
			for _, h := range r.hints {
				h.Detail = n.Label + " " + n.Value
				out = append(out, h)
			}
		}
	}
	return dedupe(out)
}

type portRule struct {
	ports []int
	hints []store.Hint
}

// portRules turn an open port into hints; ports alone that every host has
// (22, 80, 443) say nothing.
var portRules = []portRule{
	{[]int{9100, 631}, []store.Hint{hint(FieldKind, "printer", 70)}},
	{[]int{554}, []store.Hint{hint(FieldIcon, "cctv", 60), hint(FieldTag, "camera", 60)}},
	{[]int{8006}, []store.Hint{hint(FieldKind, "server", 80), hint(FieldFunction, "Proxmox VE", 80)}},
	{[]int{8123}, []store.Hint{hint(FieldFunction, "Home Assistant", 70), hint(FieldTag, "smart-home", 60)}},
	{[]int{32400}, []store.Hint{hint(FieldFunction, "Plex", 70), hint(FieldTag, "media", 60)}},
	{[]int{3389}, []store.Hint{hint(FieldKind, "computer", 60)}},
	{[]int{53}, []store.Hint{hint(FieldFunction, "DNS", 50)}},
	{[]int{1883}, []store.Hint{hint(FieldTag, "mqtt", 50)}},
}

// portHints turns recorded open ports into hints.
func portHints(open []int) []store.Hint {
	isOpen := make(map[int]bool, len(open))
	for _, p := range open {
		isOpen[p] = true
	}
	var out []store.Hint
	for _, r := range portRules {
		for _, p := range r.ports {
			if !isOpen[p] {
				continue
			}
			for _, h := range r.hints {
				h.Detail = fmt.Sprintf("port %d open", p)
				out = append(out, h)
			}
			break
		}
	}
	return dedupe(out)
}

// dedupe keeps one hint per field and value, the most confident, so a
// source's set fits device_hint's primary key.
func dedupe(hints []store.Hint) []store.Hint {
	idx := map[[2]string]int{}
	var out []store.Hint
	for _, h := range hints {
		k := [2]string{h.Field, h.Value}
		if i, ok := idx[k]; ok {
			if h.Confidence > out[i].Confidence {
				out[i] = h
			}
			continue
		}
		idx[k] = len(out)
		out = append(out, h)
	}
	return out
}
