package scan

import (
	"strings"
	"testing"
)

const arpFixture = `IP address       HW type     Flags       HW address            Mask     Device
192.168.1.1      0x1         0x2         AA:BB:CC:11:22:33     *        eth0
192.168.1.50     0x1         0x0         00:00:00:00:00:00     *        eth0
192.168.1.7      0x1         0x2         de:ad:be:ef:00:07     *        eth0
`

func TestParseARPTable(t *testing.T) {
	m := ParseARPTable(strings.NewReader(arpFixture))
	if len(m) != 2 {
		t.Fatalf("len=%d m=%v", len(m), m)
	}
	if m["192.168.1.1"] != "aa:bb:cc:11:22:33" {
		t.Errorf("gw mac=%q", m["192.168.1.1"])
	}
	if m["192.168.1.7"] != "de:ad:be:ef:00:07" {
		t.Errorf("mac=%q", m["192.168.1.7"])
	}
	if _, ok := m["192.168.1.50"]; ok {
		t.Error("incomplete entry must be skipped")
	}
}
