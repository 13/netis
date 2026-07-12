package views

// kindIcons maps a device kind to its default emoji. The device dialog
// (sub-project C) lets the user override; this is the fallback shown in the
// device list, grid, and detail views.
var kindIcons = map[string]string{
	"computer": "💻",
	"switch":   "🔀",
	"phone":    "📱",
	"server":   "🖥️",
	"printer":  "🖨️",
	"iot":      "💡",
	"vm":       "🧊",
	"lxc":      "📦",
	"wg-peer":  "🔒",
	"router":   "🛜",
	"modem":    "📶",
	"other":    "❓",
}

// KindIcon returns the default emoji for a device kind, or ❓ if unknown.
func KindIcon(kind string) string {
	if e, ok := kindIcons[kind]; ok {
		return e
	}
	return "❓"
}

// DeviceIcon returns the device's chosen icon, or the kind default when the
// device has no explicit icon set.
func DeviceIcon(icon, kind string) string {
	if icon != "" {
		return icon
	}
	return KindIcon(kind)
}

// IconChoices is the ordered emoji palette shown in the device dialog's icon
// picker. It includes every kind default (see kindIcons) plus common extras.
var IconChoices = []string{
	"💻", "🔀", "📱", "🖥️", "🖨️", "💡", "🧊", "📦", "🔒", "❓",
	"🛜", "📶", "📡", "🗄️", "📷", "🔌", "🎮", "📺", "☎️", "🕹️", "🛰️", "⌚",
}
