package macaddr

import "testing"

func TestIsPrivate(t *testing.T) {
	cases := map[string]bool{
		"da:a1:19:00:00:01": true,  // 0xda: locally administered
		"36:0e:7c:aa:bb:cc": true,  // 0x36
		"AE:12:34:56:78:9A": true,  // upper case
		"f2-11-22-33-44-55": true,  // dash separated
		"bc:24:11:00:00:01": false, // Proxmox OUI, universally administered
		"00:11:22:33:44:55": false,
		"52:54:00:12:34:56": false, // QEMU/KVM default prefix
		"02:42:ac:11:00:02": false, // Docker
		"03:00:00:00:00:01": false, // multicast
		"":                  false,
		"zz:00:00:00:00:00": false,
	}
	for mac, want := range cases {
		if got := IsPrivate(mac); got != want {
			t.Errorf("IsPrivate(%q)=%v want %v", mac, got, want)
		}
	}
	if !AnyPrivate([]string{"00:11:22:33:44:55", "da:a1:19:00:00:01"}) || AnyPrivate(nil) {
		t.Error("AnyPrivate")
	}
}
