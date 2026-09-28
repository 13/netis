// Package macaddr holds small facts about MAC addresses that the scanner, the
// API and the views all need, computed from the address alone.
package macaddr

import (
	"strconv"
	"strings"
)

// stablePrefixes are locally administered prefixes handed out by
// virtualization stacks. They are fixed per guest, not randomized, so they
// must not read as a phone's private address.
var stablePrefixes = []string{
	"52:54:00", // QEMU/KVM
	"02:42:",   // Docker bridge
}

// IsPrivate reports whether mac is a locally administered address of the kind
// phones and laptops randomize per network ("Private Wi-Fi Address",
// "Randomized MAC"): bit 0x02 of the first octet set. Multicast addresses and
// the stable virtualization prefixes above are not private.
func IsPrivate(mac string) bool {
	mac = strings.ToLower(strings.ReplaceAll(mac, "-", ":"))
	if len(mac) < 2 {
		return false
	}
	first, err := strconv.ParseUint(mac[:2], 16, 8)
	if err != nil || first&0x01 != 0 || first&0x02 == 0 {
		return false
	}
	for _, p := range stablePrefixes {
		if strings.HasPrefix(mac, p) {
			return false
		}
	}
	return true
}

// AnyPrivate reports whether any of macs is private.
func AnyPrivate(macs []string) bool {
	for _, m := range macs {
		if IsPrivate(m) {
			return true
		}
	}
	return false
}
