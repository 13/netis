package oui

import (
	"regexp"
	"strings"
)

// suffixRe matches one trailing legal-form or filler word. Normalize strips
// it repeatedly, so "TP-LINK TECHNOLOGIES CO.,LTD." loses ",LTD.", then
// " CO.", then " TECHNOLOGIES".
var suffixRe = regexp.MustCompile(`(?i)[\s,.]+(inc|incorporated|corp|corporate|corporation|co|company|ltd|limited|llc|l\.l\.c|gmbh|ag|s\.?a|s\.?p\.?a|s\.?a\.?s|b\.?v|n\.?v|oy|ab|a/s|kg|plc|pty|pte|s\.?r\.?l|k\.?k|sdn|bhd|technologies|technology|electronics|international|ind)\.?$`)

var spaceRe = regexp.MustCompile(`\s+`)

// overrides maps a stripped, lower-cased registry name to the name people
// know the maker by.
var overrides = map[string]string{
	"raspberry pi trading":       "Raspberry Pi",
	"raspberry pi foundation":    "Raspberry Pi",
	"hon hai precision":          "Foxconn",
	"hon hai precision industry": "Foxconn",
	"tp-link":                    "TP-Link",
	"tp-link systems":            "TP-Link",
	"asustek computer":           "ASUS",
	"intel":                      "Intel",
	"hewlett packard":            "HP",
	"hewlett packard enterprise": "HPE",
	"ubiquiti networks":          "Ubiquiti",
	"xiaomi communications":      "Xiaomi",
	"seiko epson":                "Epson",
	"brother industries":         "Brother",
	"murata manufacturing":       "Murata",
	"allterco robotics eood":     "Shelly",
	"avm audiovisuelles marketing und computersysteme": "AVM",
	"zte":                                 "ZTE",
	"annapurna labs":                      "Annapurna Labs",
	"motorola mobility llc, a lenovo":     "Motorola Mobility",
	"beijing xiaomi mobile software":      "Xiaomi",
	"routerboard.com":                     "MikroTik",
	"shenzhen gongjin electronics co.,lt": "Shenzhen Gongjin Electronics",
	"espressif systems (singapore)":       "Espressif",
	"shelly europe":                       "Shelly",
}

// Normalize turns an IEEE registry organisation name into a short vendor
// name: whitespace collapsed, legal suffixes stripped (never down to
// nothing), well-known makers renamed, and ALL-CAPS names title-cased.
func Normalize(name string) string {
	s := strings.TrimSpace(spaceRe.ReplaceAllString(name, " "))
	for {
		loc := suffixRe.FindStringIndex(s)
		if loc == nil || loc[0] == 0 {
			break
		}
		s = strings.TrimRight(s[:loc[0]], " ,.")
	}
	if o, ok := overrides[strings.ToLower(s)]; ok {
		return o
	}
	if s != strings.ToUpper(s) || len(s) <= 4 {
		return s
	}
	words := strings.Split(s, " ")
	for i, w := range words {
		if len(w) > 3 {
			words[i] = w[:1] + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}
