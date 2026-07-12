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
	"other":    "❓",
}

// KindIcon returns the default emoji for a device kind, or ❓ if unknown.
func KindIcon(kind string) string {
	if e, ok := kindIcons[kind]; ok {
		return e
	}
	return "❓"
}
