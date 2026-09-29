package views

import (
	"os"
	"strings"
	"testing"
)

func TestDeviceIcon(t *testing.T) {
	cases := []struct{ icon, kind, want string }{
		{"gamepad-2", "phone", "gamepad-2"},  // explicit choice wins
		{"", "phone", "smartphone"},          // no choice: kind default
		{"", "nonsense", "circle-help"},      // unknown kind
		{"🎮", "phone", "gamepad-2"},          // stored emoji keeps the owner's pick
		{"🖥️", "computer", "server"},          // with the variation selector
		{"🖥", "computer", "server"},          // and without
		{"🗄️", "server", "hard-drive"},        // an extra from the old palette
		{"not-an-icon", "router", "router"},  // anything else: kind default
		{"<script>", "nonsense", "circle-help"},
	}
	for _, c := range cases {
		if got := DeviceIcon(c.icon, c.kind); got != c.want {
			t.Errorf("DeviceIcon(%q, %q) = %q, want %q", c.icon, c.kind, got, c.want)
		}
	}
}

func TestKindIcon(t *testing.T) {
	cases := map[string]string{
		"phone": "smartphone", "computer": "laptop", "server": "server",
		"switch": "ethernet-port", "printer": "printer", "iot": "lightbulb",
		"vm": "box", "lxc": "container", "wg-peer": "lock", "other": "circle-help",
		"router": "router", "modem": "signal",
		"nonsense": "circle-help", "": "circle-help",
	}
	for kind, want := range cases {
		if got := KindIcon(kind); got != want {
			t.Errorf("KindIcon(%q) = %q, want %q", kind, got, want)
		}
	}
}

// Every icon the views can name exists in the sprite, and every old emoji
// choice still has a replacement.
func TestIconsExistInSprite(t *testing.T) {
	b, err := os.ReadFile("../static/icons.svg")
	if err != nil {
		t.Fatal(err)
	}
	sprite := string(b)
	has := func(n string) bool { return strings.Contains(sprite, `<symbol id="`+n+`"`) }
	for _, n := range IconChoices {
		if !has(n) {
			t.Errorf("picker icon %q missing from icons.svg", n)
		}
	}
	for k, n := range kindIcons {
		if !has(n) {
			t.Errorf("kind %q icon %q missing from icons.svg", k, n)
		}
		if !choiceSet[n] {
			t.Errorf("kind %q icon %q is not offered in the picker", k, n)
		}
	}
	for e, n := range legacyIcons {
		if !choiceSet[n] {
			t.Errorf("legacy %q maps to %q, not a picker choice", e, n)
		}
	}
	for _, n := range uiIcons {
		if !has(n) {
			t.Errorf("interface icon %q missing from icons.svg", n)
		}
	}
}
