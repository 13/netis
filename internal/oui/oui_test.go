package oui

import "testing"

func TestLookupLongestPrefix(t *testing.T) {
	table := map[string]string{"AABBCC": "Large", "AABBCC1": "Medium", "AABBCC123": "Small"}
	cases := map[string]string{
		"aa:bb:cc:00:00:01": "Large",
		"aa:bb:cc:10:00:01": "Medium",
		"AA-BB-CC-12-34-56": "Small",
		"aabbcc123fff":      "Small",
		"dd:ee:ff:00:00:00": "",
		"":                  "",
		"zz":                "",
	}
	for mac, want := range cases {
		if got := lookup(table, mac); got != want {
			t.Errorf("lookup(%q) = %q, want %q", mac, got, want)
		}
	}
}

func TestVendorRealRegistry(t *testing.T) {
	cases := map[string]string{
		"bc:24:11:aa:bb:cc": "Proxmox Server Solutions",
		"b8:27:eb:00:00:01": "Raspberry Pi",
		"3c:07:54:00:00:01": "Apple",
		"52:54:00:12:34:56": "QEMU",
		"00:15:5d:00:00:01": "Microsoft Hyper-V",
		// Locally administered (randomized) MACs have no vendor.
		"da:a1:19:00:00:01": "",
	}
	for mac, want := range cases {
		if got := Vendor(mac); got != want {
			t.Errorf("Vendor(%q) = %q, want %q", mac, got, want)
		}
	}
}
