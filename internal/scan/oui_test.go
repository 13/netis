package scan

import "testing"

func TestVendorLookup(t *testing.T) {
	// BC:24:11 is in oui_data.txt as Proxmox Server Solutions GmbH
	if v := Vendor("bc:24:11:aa:bb:cc"); v != "Proxmox Server Solutions GmbH" {
		t.Errorf("got %q", v)
	}
	if v := Vendor("ff:ff:ff:00:00:00"); v != "" {
		t.Errorf("unknown OUI should be empty, got %q", v)
	}
}
