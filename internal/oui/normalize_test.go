package oui

import "testing"

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"Apple, Inc.":                     "Apple",
		"TP-LINK TECHNOLOGIES CO.,LTD.":   "TP-Link",
		"Samsung Electronics Co.,Ltd":     "Samsung",
		"Raspberry Pi Trading Ltd":        "Raspberry Pi",
		"Raspberry Pi Foundation":         "Raspberry Pi",
		"Proxmox Server Solutions GmbH":   "Proxmox Server Solutions",
		"Espressif Inc.":                  "Espressif",
		"Synology Incorporated":           "Synology",
		"ASUSTek COMPUTER INC.":           "ASUS",
		"Hon Hai Precision Ind. Co.,Ltd.": "Foxconn",
		"Seiko Epson Corporation":         "Epson",
		"Brother Industries, LTD.":        "Brother",
		"Ubiquiti Inc":                    "Ubiquiti",
		"  Sony   Corporation ":           "Sony",
		"NORDIC SEMICONDUCTOR ASA":        "Nordic Semiconductor ASA",
		"Electronics Ltd":                 "Electronics",
		// Real registry entries observed to come out ugly without an
		// override: already-lower-case brand names, a dangling "a Lenovo"
		// after stripping "Company", a Xiaomi subsidiary, MikroTik's
		// registered trading name, and a truncated "LT" (source typo for
		// "LTD") that survives suffix stripping.
		"zte corporation":                         "ZTE",
		"Annapurna labs":                          "Annapurna Labs",
		"Motorola Mobility LLC, a Lenovo Company": "Motorola Mobility",
		"Beijing Xiaomi Mobile Software Co., Ltd": "Xiaomi",
		"Routerboard.com":                         "MikroTik",
		"SHENZHEN GONGJIN ELECTRONICS CO.,LT":     "Shenzhen Gongjin Electronics",
		"Renesas Electronics (Penang) Sdn. Bhd.":  "Renesas Electronics (Penang)",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
