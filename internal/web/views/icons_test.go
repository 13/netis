package views

import "testing"

func TestDeviceIcon(t *testing.T) {
	if got := DeviceIcon("🎮", "phone"); got != "🎮" {
		t.Errorf("explicit icon should win, got %q", got)
	}
	if got := DeviceIcon("", "phone"); got != "📱" {
		t.Errorf("empty icon falls back to kind default, got %q", got)
	}
	if got := DeviceIcon("", "nonsense"); got != "❓" {
		t.Errorf("unknown kind falls back to ❓, got %q", got)
	}
}

func TestKindIcon(t *testing.T) {
	cases := map[string]string{
		"phone": "📱", "computer": "💻", "server": "🖥️",
		"switch": "🔀", "printer": "🖨️", "iot": "💡",
		"vm": "🧊", "lxc": "📦", "wg-peer": "🔒", "other": "❓",
		"router": "🛜", "modem": "📶",
		"nonsense": "❓", "": "❓",
	}
	for kind, want := range cases {
		if got := KindIcon(kind); got != want {
			t.Errorf("KindIcon(%q) = %q, want %q", kind, got, want)
		}
	}
}
