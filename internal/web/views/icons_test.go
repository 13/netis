package views

import "testing"

func TestKindIcon(t *testing.T) {
	cases := map[string]string{
		"phone": "📱", "computer": "💻", "server": "🖥️",
		"switch": "🔀", "printer": "🖨️", "iot": "💡",
		"vm": "🧊", "lxc": "📦", "wg-peer": "🔒", "other": "❓",
		"nonsense": "❓", "": "❓",
	}
	for kind, want := range cases {
		if got := KindIcon(kind); got != want {
			t.Errorf("KindIcon(%q) = %q, want %q", kind, got, want)
		}
	}
}
