package views

import (
	"testing"

	"netis/internal/store"
)

func TestLowestIPStr(t *testing.T) {
	cases := []struct {
		name string
		ips  []store.IPInfo
		want string
	}{
		{
			name: "non-ascending insertion order picks numeric lowest",
			ips: []store.IPInfo{
				{IP: "10.0.0.50", Kind: "dhcp"},
				{IP: "10.0.0.5", Kind: "static"},
			},
			want: "10.0.0.5",
		},
		{
			name: "already-lowest-first stays lowest",
			ips: []store.IPInfo{
				{IP: "10.0.0.2", Kind: "static"},
				{IP: "10.0.0.100", Kind: "dhcp"},
			},
			want: "10.0.0.2",
		},
		{
			name: "unparseable falls back to first raw value",
			ips: []store.IPInfo{
				{IP: "not-an-ip", Kind: "static"},
			},
			want: "not-an-ip",
		},
		{
			name: "no IPs returns empty string",
			ips:  nil,
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lowestIPStr(c.ips); got != c.want {
				t.Errorf("lowestIPStr(%+v) = %q, want %q", c.ips, got, c.want)
			}
		})
	}
}
