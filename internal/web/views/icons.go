package views

import "strings"

// Icons are Lucide symbols in the sprite at /static/icons.svg (built by
// internal/web/gen_icons.go); a name here is a symbol id there.

// unknownIcon is drawn for a kind netis does not know, in the muted colour so
// it never reads as an alert.
const unknownIcon = "circle-help"

// kindIcons maps a device kind to its default icon. The device dialog lets
// the user override; this is the fallback shown in the device list, tiles and
// detail views.
var kindIcons = map[string]string{
	"computer": "laptop",
	"switch":   "ethernet-port",
	"phone":    "smartphone",
	"server":   "server",
	"printer":  "printer",
	"iot":      "lightbulb",
	"vm":       "box",
	"lxc":      "container",
	"wg-peer":  "lock",
	"router":   "router",
	"modem":    "signal",
	"other":    unknownIcon,
}

// KindIcon returns the default icon for a device kind, or the unknown icon.
func KindIcon(kind string) string {
	if n, ok := kindIcons[kind]; ok {
		return n
	}
	return unknownIcon
}

// IconChoices is the ordered palette shown in the device dialog's icon
// picker. It includes every kind default (see kindIcons) plus common extras.
var IconChoices = []string{
	"laptop", "monitor", "ethernet-port", "router", "signal", "wifi", "smartphone",
	"tablet", "server", "hard-drive", "printer", "lightbulb", "box", "container",
	"lock", "satellite-dish", "cctv", "plug", "gamepad-2", "tv", "phone",
	"joystick", "satellite", "watch", "speaker", "thermometer", "house", unknownIcon,
}

// legacyIcons maps the emoji the icon picker stored before icons were SVG to
// the icon that replaces each, so a device keeps the icon its owner picked.
// Keys have no variation selector (U+FE0F); see DeviceIcon.
var legacyIcons = map[string]string{
	"💻": "laptop", "🔀": "ethernet-port", "📱": "smartphone", "🖥": "server",
	"🖨": "printer", "💡": "lightbulb", "🧊": "box", "📦": "container",
	"🔒": "lock", "❓": unknownIcon, "🛜": "router", "📶": "signal",
	"📡": "satellite-dish", "🗄": "hard-drive", "📷": "cctv", "🔌": "plug",
	"🎮": "gamepad-2", "📺": "tv", "☎": "phone", "🕹": "joystick",
	"🛰": "satellite", "⌚": "watch",
}

var choiceSet = func() map[string]bool {
	m := make(map[string]bool, len(IconChoices))
	for _, n := range IconChoices {
		m[n] = true
	}
	return m
}()

// DeviceIcon returns the icon to draw for a device: its chosen icon, an old
// emoji choice translated, or the kind default when it has none (or has a
// value that names no icon, as the API accepts any string).
func DeviceIcon(icon, kind string) string {
	if choiceSet[icon] {
		return icon
	}
	if n, ok := legacyIcons[strings.ReplaceAll(icon, "️", "")]; ok {
		return n
	}
	return KindIcon(kind)
}

// iconLabel names an icon for the picker's accessible labels: "gamepad-2"
// reads as "gamepad".
func iconLabel(name string) string {
	if name == unknownIcon {
		return "unknown"
	}
	name = strings.TrimSuffix(name, "-2")
	return strings.ReplaceAll(name, "-", " ")
}

// uiIcons are the interface icons in the sprite, shown on /styleguide.
var uiIcons = []string{
	"search", "plus", "radar", "refresh-cw", "settings", "user", "log-out", "sun",
	"moon", "monitor", "chevron-down", "chevron-right", "x", "check", "triangle-alert",
	"trash-2", "pencil", "external-link", "copy", "power", "wifi", "network", "server",
	"shield", "bell", "key", "list", "layout-grid", "clock", "filter", "ellipsis",
	"arrow-left", "download", "upload", "circle-help", "layout-dashboard", "activity",
	"sun-moon", "keyboard", "corner-down-left", "menu",
}

// iconTone is the extra class for a device icon: the unknown icon is muted.
func iconTone(name string) string {
	if name == unknownIcon {
		return "icon-unknown"
	}
	return ""
}
