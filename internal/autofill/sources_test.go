package autofill

import (
	"testing"

	"netis/internal/store"
)

// has reports whether hints contain field=value at confidence conf.
func has(hints []store.Hint, field, value string, conf int) bool {
	for _, h := range hints {
		if h.Field == field && h.Value == value && h.Confidence == conf {
			return true
		}
	}
	return false
}

func TestOUIHints(t *testing.T) {
	hs := ouiHints([]string{"3c:07:54:00:00:01", "da:a1:19:00:00:01", ""})
	if len(hs) != 1 || !has(hs, "vendor", "Apple", 90) || hs[0].Detail != "MAC 3c:07:54:00:00:01" {
		t.Fatalf("hints = %+v", hs)
	}
	// Espressif makes one thing: kind iot as a suggestion (40).
	hs = ouiHints([]string{"24:0a:c4:00:00:01"})
	if !has(hs, "vendor", "Espressif", 90) || !has(hs, "kind", "iot", 40) {
		t.Fatalf("hints = %+v", hs)
	}
}

func TestHostnameRules(t *testing.T) {
	type want struct {
		field, value string
		conf         int
	}
	cases := map[string][]want{
		"Bens-iPhone":         {{"kind", "phone", 70}, {"vendor", "Apple", 70}, {"model", "iPhone", 60}},
		"iPad.lan":            {{"kind", "phone", 60}, {"icon", "tablet", 70}, {"model", "iPad", 60}},
		"MacBook-Pro-3.local": {{"kind", "computer", 70}, {"model", "MacBook Pro", 65}},
		"MacBook-Air":         {{"model", "MacBook Air", 65}},
		"imac":                {{"model", "iMac", 60}},
		"Mac-mini":            {{"model", "Mac mini", 60}},
		"Galaxy-S23":          {{"kind", "phone", 70}, {"vendor", "Samsung", 70}},
		"SM-G991B":            {{"vendor", "Samsung", 70}},
		"Pixel-8":             {{"vendor", "Google", 70}},
		"android-3f2a9c":      {{"kind", "phone", 60}},
		"DESKTOP-AB12CD3":     {{"kind", "computer", 70}},
		"LAPTOP-9K2M4X1":      {{"kind", "computer", 70}},
		"BRW3C2AF4A1B2C3":     {{"kind", "printer", 80}, {"vendor", "Brother", 80}},
		"HP3C2AF4":            {{"kind", "printer", 80}, {"vendor", "HP", 80}},
		"NPI3C2AF4":           {{"vendor", "HP", 80}},
		"EPSON1A2B3C":         {{"kind", "printer", 70}, {"vendor", "Epson", 70}},
		"ESP-1A2B3C":          {{"kind", "iot", 70}, {"tag", "smart-home", 60}},
		"shelly1pm-ABC":       {{"kind", "iot", 70}},
		"tasmota-1234":        {{"kind", "iot", 70}},
		"Chromecast":          {{"icon", "tv", 70}, {"tag", "media", 60}},
		"Apple-TV":            {{"icon", "tv", 70}},
		"Sonos-Kitchen":       {{"icon", "speaker", 70}, {"vendor", "Sonos", 70}},
		"PS5-123":             {{"icon", "gamepad-2", 70}},
		"XBOX":                {{"icon", "gamepad-2", 70}},
		"raspberrypi":         {{"kind", "computer", 60}, {"vendor", "Raspberry Pi", 60}},
		"diskstation":         {{"kind", "server", 60}, {"tag", "nas", 60}},
		"nas01":               {{"tag", "nas", 60}},
		"pve":                 {{"kind", "server", 70}, {"function", "Proxmox VE", 60}},
		"pve2.home.arpa":      {{"function", "Proxmox VE", 60}},
	}
	for hn, wants := range cases {
		hs := hostnameHints([]named{{Label: "hostname", Value: hn}})
		for _, w := range wants {
			if !has(hs, w.field, w.value, w.conf) {
				t.Errorf("%s: missing %s=%s@%d in %+v", hn, w.field, w.value, w.conf, hs)
			}
		}
	}
	for _, hn := range []string{"dynasty", "unknown-bc:24:11:00:00:01", "private-da:a1:19:00:00:01", "pihole-aa:bb:cc:dd:ee:ff", "printer-room", "espresso"} {
		if hs := hostnameHints([]named{{Label: "hostname", Value: hn}}); len(hs) != 0 {
			t.Errorf("%s: unexpected hints %+v", hn, hs)
		}
	}
	hs := hostnameHints([]named{{Label: "hostname", Value: "BRW3C2AF4A1B2C3"}})
	if hs[0].Detail != `hostname BRW3C2AF4A1B2C3` {
		t.Errorf("detail = %q", hs[0].Detail)
	}
}

func TestPortHints(t *testing.T) {
	hs := portHints([]int{22, 9100, 8006, 53})
	if !has(hs, "kind", "printer", 70) || !has(hs, "kind", "server", 80) ||
		!has(hs, "function", "Proxmox VE", 80) || !has(hs, "function", "DNS", 50) {
		t.Fatalf("hints = %+v", hs)
	}
	for _, h := range hs {
		if h.Field == "kind" && h.Value == "printer" && h.Detail != "port 9100 open" {
			t.Errorf("detail = %q", h.Detail)
		}
	}
	if hs := portHints([]int{22, 443}); len(hs) != 0 {
		t.Errorf("ssh/https alone say nothing: %+v", hs)
	}
}

func TestDedupeKeepsHighest(t *testing.T) {
	got := dedupe([]store.Hint{
		{Field: "kind", Value: "server", Confidence: 60, Detail: "a"},
		{Field: "kind", Value: "server", Confidence: 70, Detail: "b"},
		{Field: "tag", Value: "nas", Confidence: 60},
	})
	if len(got) != 2 || !has(got, "kind", "server", 70) {
		t.Fatalf("dedupe = %+v", got)
	}
}
