package autofill

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"netis/internal/oui"
	"netis/internal/probe"
	"netis/internal/store"
)

// appleFamily is one Apple model-id family: a prefix, the name people know it
// by, and whether it is one of the Mac lines (for device-info's kind rule).
type appleFamily struct {
	prefix, name string
	mac          bool
}

// appleFamilies are matched longest-prefix-first, since e.g. "MacBook" is a
// prefix of "MacBookPro".
var appleFamilies = []appleFamily{
	{"MacBookPro", "MacBook Pro", true},
	{"MacBookAir", "MacBook Air", true},
	{"MacBook", "MacBook", true},
	{"iMac", "iMac", true},
	{"Macmini", "Mac mini", true},
	{"MacPro", "Mac Pro", true},
	{"AppleTV", "Apple TV", false},
	{"AudioAccessory", "HomePod", false},
	{"iPhone", "iPhone", false},
	{"iPad", "iPad", false},
	{"Watch", "Apple Watch", false},
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// matchAppleFamily finds the family whose prefix matches id, immediately
// followed by a digit (so "MacBookProMax" does not match "MacBookPro").
func matchAppleFamily(id string) (appleFamily, bool) {
	for _, f := range appleFamilies {
		if !strings.HasPrefix(id, f.prefix) {
			continue
		}
		rest := id[len(f.prefix):]
		if rest != "" && isDigit(rest[0]) {
			return f, true
		}
	}
	return appleFamily{}, false
}

// appleModel turns a model id into a human name: "MacBook Pro
// (MacBookPro18,3)". An id that matches no known family (e.g. "Mac14,2", an
// unknown id, or a family name not followed by a digit) is returned
// unchanged; an empty id returns empty.
func appleModel(id string) string {
	if id == "" {
		return ""
	}
	if f, ok := matchAppleFamily(id); ok {
		return f.name + " (" + id + ")"
	}
	return id
}

// isApplePrefixed reports whether id names an Apple device: one of
// appleFamilies' prefixes followed by a digit, or bare "Mac" followed by a
// digit (e.g. "Mac14,2", which appleModel leaves unchanged).
func isApplePrefixed(id string) bool {
	if id == "" {
		return false
	}
	if _, ok := matchAppleFamily(id); ok {
		return true
	}
	return strings.HasPrefix(id, "Mac") && len(id) > 3 && isDigit(id[3])
}

// isMacPrefixed reports whether id is one of the Mac computer lines (not
// AppleTV, AudioAccessory, iPhone, iPad or Watch), for device-info's kind
// rule.
func isMacPrefixed(id string) bool {
	if id == "" {
		return false
	}
	if f, ok := matchAppleFamily(id); ok {
		return f.mac
	}
	return strings.HasPrefix(id, "Mac") && len(id) > 3 && isDigit(id[3])
}

// firstNonEmpty returns the first trimmed, non-empty value.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

// hapIcons maps HomeKit's "ci" (category identifier) TXT value to an icon.
// 32 (Target Controller, a remote) is left out: it is not a TV.
var hapIcons = map[string]string{
	"5": "lightbulb", "7": "plug", "8": "plug",
	"9": "thermometer", "10": "thermometer",
	"17": "cctv", "18": "cctv", // IP camera, video doorbell
	"24": "tv", "31": "tv", "35": "tv", "36": "tv", // Apple TV, television, set-top box, streaming stick
	"25": "speaker", "26": "speaker", "34": "speaker", // HomePod, speaker, audio receiver
}

// hapBridge is HomeKit's category for a bridge. Bridges such as Home
// Assistant's HomeKit Bridge and Homebridge run on servers, so they say
// nothing about the host but that it does smart-home work.
const hapBridge = "2"

// castSpeaker matches the Google Cast model names of speakers and speaker
// groups, as opposed to TVs and streamers.
var castSpeaker = regexp.MustCompile(`(?i)nest (mini|audio)|google home|home mini|audio|speaker|group`)

// mediaSoftware matches the UPnP model names of media software running on a
// general-purpose machine, which say nothing about the machine itself.
var mediaSoftware = regexp.MustCompile(`(?i)windows media player|windows media|plex|kodi|minidlna|readydlna|jellyfin|emby|serviio|universal media server`)

// workstationMAC matches the " [xx:xx:xx:xx:xx:xx]" suffix _workstation._tcp
// instances carry.
var workstationMAC = regexp.MustCompile(`(?i)\s\[[0-9a-f]{2}(:[0-9a-f]{2}){5}\]$`)

func hGooglecast(s probe.MDNSService) []store.Hint {
	var hs []store.Hint
	if v := s.TXT["md"]; v != "" {
		hs = append(hs, hint(FieldModel, v, 80))
	}
	if name := firstNonEmpty(s.TXT["fn"], s.Instance); name != "" {
		hs = append(hs, hint(FieldName, name, 70))
	}
	icon := "tv"
	if castSpeaker.MatchString(s.TXT["md"]) {
		icon = "speaker"
	}
	hs = append(hs, hint(FieldIcon, icon, 60), hint(FieldTag, "media", 60))
	return hs
}

func hAirplay(s probe.MDNSService) []store.Hint {
	var hs []store.Hint
	if model := s.TXT["model"]; model != "" {
		hs = append(hs, hint(FieldModel, appleModel(model), 70))
		if isApplePrefixed(model) {
			hs = append(hs, hint(FieldVendor, "Apple", 70))
		}
		switch {
		case strings.HasPrefix(model, "AppleTV"):
			hs = append(hs, hint(FieldIcon, "tv", 70))
		case strings.HasPrefix(model, "AudioAccessory"):
			hs = append(hs, hint(FieldIcon, "speaker", 70))
		}
	}
	if name := strings.TrimSpace(s.Instance); name != "" {
		hs = append(hs, hint(FieldName, name, 60))
	}
	return hs
}

func hRaop(s probe.MDNSService) []store.Hint {
	var hs []store.Hint
	if am := s.TXT["am"]; am != "" {
		hs = append(hs, hint(FieldModel, appleModel(am), 60))
	}
	if _, after, ok := strings.Cut(s.Instance, "@"); ok {
		if name := strings.TrimSpace(after); name != "" {
			hs = append(hs, hint(FieldName, name, 50))
		}
	}
	return hs
}

func hDeviceInfo(s probe.MDNSService) []store.Hint {
	var hs []store.Hint
	model := s.TXT["model"]
	if model == "" {
		return hs
	}
	hs = append(hs, hint(FieldModel, appleModel(model), 70))
	if isApplePrefixed(model) {
		hs = append(hs, hint(FieldVendor, "Apple", 70))
	}
	if isMacPrefixed(model) {
		hs = append(hs, hint(FieldKind, "computer", 60))
	}
	return hs
}

func hCompanionLink(s probe.MDNSService) []store.Hint {
	hs := []store.Hint{hint(FieldVendor, "Apple", 70)}
	rpmd := s.TXT["rpmd"]
	if rpmd == "" {
		return hs
	}
	hs = append(hs, hint(FieldModel, appleModel(rpmd), 70))
	if strings.HasPrefix(rpmd, "iPhone") || strings.HasPrefix(rpmd, "iPad") {
		hs = append(hs, hint(FieldKind, "phone", 70))
	}
	if strings.HasPrefix(rpmd, "iPad") {
		hs = append(hs, hint(FieldIcon, "tablet", 70))
	}
	return hs
}

func hPrinter(s probe.MDNSService) []store.Hint {
	hs := []store.Hint{hint(FieldKind, "printer", 90)}
	if m := s.TXT["usb_mdl"]; m != "" {
		hs = append(hs, hint(FieldModel, m, 80))
	} else if t := s.TXT["ty"]; t != "" {
		hs = append(hs, hint(FieldModel, t, 75))
	}
	if mfg := s.TXT["usb_mfg"]; mfg != "" {
		if v := oui.Normalize(mfg); v != "" {
			hs = append(hs, hint(FieldVendor, v, 80))
		}
	}
	if name := strings.TrimSpace(s.Instance); name != "" {
		hs = append(hs, hint(FieldName, name, 60))
	}
	return hs
}

func hHap(s probe.MDNSService) []store.Hint {
	if s.TXT["ci"] == hapBridge {
		return []store.Hint{hint(FieldTag, "smart-home", 60)}
	}
	hs := []store.Hint{hint(FieldKind, "iot", 70), hint(FieldTag, "smart-home", 60)}
	if md := s.TXT["md"]; md != "" {
		hs = append(hs, hint(FieldModel, md, 70))
	}
	if icon, ok := hapIcons[s.TXT["ci"]]; ok {
		hs = append(hs, hint(FieldIcon, icon, 70))
	}
	if name := strings.TrimSpace(s.Instance); name != "" {
		hs = append(hs, hint(FieldName, name, 60))
	}
	return hs
}

func hHue(probe.MDNSService) []store.Hint {
	return []store.Hint{
		hint(FieldVendor, "Philips Hue", 80),
		hint(FieldKind, "iot", 80),
		hint(FieldModel, "Hue Bridge", 70),
		hint(FieldTag, "smart-home", 60),
	}
}

func hESPHome(s probe.MDNSService) []store.Hint {
	hs := []store.Hint{
		hint(FieldKind, "iot", 80),
		hint(FieldFunction, "ESPHome", 70),
		hint(FieldTag, "smart-home", 60),
	}
	if name := firstNonEmpty(s.TXT["friendly_name"], s.Instance); name != "" {
		hs = append(hs, hint(FieldName, name, 60))
	}
	return hs
}

func hHomeAssistant(probe.MDNSService) []store.Hint {
	return []store.Hint{
		hint(FieldKind, "server", 60),
		hint(FieldFunction, "Home Assistant", 90),
		hint(FieldTag, "smart-home", 60),
	}
}

func hSMB(probe.MDNSService) []store.Hint {
	return []store.Hint{hint(FieldTag, "file-share", 50)}
}

func hSonos(probe.MDNSService) []store.Hint {
	return []store.Hint{
		hint(FieldVendor, "Sonos", 90),
		hint(FieldIcon, "speaker", 80),
		hint(FieldTag, "media", 60),
	}
}

func hSpotifyConnect(probe.MDNSService) []store.Hint {
	return []store.Hint{hint(FieldTag, "media", 50)}
}

func hWorkstation(s probe.MDNSService) []store.Hint {
	hs := []store.Hint{hint(FieldKind, "computer", 60)}
	name := strings.TrimSpace(workstationMAC.ReplaceAllString(s.Instance, ""))
	if name != "" {
		hs = append(hs, hint(FieldName, name, 50))
	}
	return hs
}

// mdnsHandlers maps a service type to the function that turns one instance
// of it into hints.
var mdnsHandlers = map[string]func(probe.MDNSService) []store.Hint{
	"_googlecast._tcp":      hGooglecast,
	"_airplay._tcp":         hAirplay,
	"_raop._tcp":            hRaop,
	"_device-info._tcp":     hDeviceInfo,
	"_companion-link._tcp":  hCompanionLink,
	"_ipp._tcp":             hPrinter,
	"_ipps._tcp":            hPrinter,
	"_printer._tcp":         hPrinter,
	"_pdl-datastream._tcp":  hPrinter,
	"_hap._tcp":             hHap,
	"_hue._tcp":             hHue,
	"_esphomelib._tcp":      hESPHome,
	"_home-assistant._tcp":  hHomeAssistant,
	"_smb._tcp":             hSMB,
	"_sonos._tcp":           hSonos,
	"_spotify-connect._tcp": hSpotifyConnect,
	"_workstation._tcp":     hWorkstation,
}

// maxAnnounced caps a hint value taken from what a device announced, in runes.
const maxAnnounced = 128

// cleanAnnounced tidies a string a device announced: control and format
// characters (bidi overrides, zero-width spaces, which can make a name
// display as something else) dropped, whitespace runs collapsed to one space, trimmed, and capped at
// maxAnnounced runes.
func cleanAnnounced(v string) string {
	v = strings.Map(func(r rune) rune {
		if (unicode.IsControl(r) && !unicode.IsSpace(r)) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, v)
	v = strings.Join(strings.Fields(v), " ")
	if r := []rune(v); len(r) > maxAnnounced {
		v = strings.TrimSpace(string(r[:maxAnnounced]))
	}
	return v
}

// finish cleans every hint value with cleanAnnounced, drops the hints left
// empty, and tags the rest with detail.
func finish(hs []store.Hint, detail string) []store.Hint {
	out := hs[:0]
	for _, h := range hs {
		h.Value = cleanAnnounced(h.Value)
		if h.Value == "" {
			continue
		}
		h.Detail = detail
		out = append(out, h)
	}
	return out
}

// mdnsHints turns every service of one address into hints, each tagged with
// evidence naming its type and instance.
func mdnsHints(svcs []probe.MDNSService) []store.Hint {
	var out []store.Hint
	for _, s := range svcs {
		fn, ok := mdnsHandlers[s.Type]
		if !ok {
			continue
		}
		out = append(out, finish(fn(s), fmt.Sprintf("mDNS %s %q", s.Type, s.Instance))...)
	}
	return dedupe(out)
}

// deviceTypeShort returns the part of a UPnP deviceType URN after "device:"
// up to the next ":", e.g. "MediaRenderer" from
// "urn:schemas-upnp-org:device:MediaRenderer:1".
func deviceTypeShort(t string) string {
	const marker = "device:"
	i := strings.Index(t, marker)
	if i < 0 {
		return ""
	}
	rest := t[i+len(marker):]
	if j := strings.Index(rest, ":"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// isMediaSoftware reports whether d is a media renderer or server that is
// software on a general-purpose machine (Windows Media Player, Plex, Kodi
// and the like) rather than a media device.
func isMediaSoftware(d probe.UPnPDevice) bool {
	switch deviceTypeShort(d.DeviceType) {
	case "MediaRenderer", "MediaServer":
	default:
		return false
	}
	return strings.EqualFold(oui.Normalize(d.Manufacturer), "Microsoft") || mediaSoftware.MatchString(d.ModelName)
}

// deviceHints turns one UPnP device description into hints. Media software
// gives no vendor or model: those would describe the software, not the host.
func deviceHints(d probe.UPnPDevice) []store.Hint {
	var hs []store.Hint
	software := isMediaSoftware(d)
	if d.Manufacturer != "" && !software {
		if v := oui.Normalize(d.Manufacturer); v != "" {
			hs = append(hs, hint(FieldVendor, v, 80))
		}
	}
	if d.ModelName != "" && !software {
		model := d.ModelName
		if d.ModelNumber != "" && !strings.Contains(model, d.ModelNumber) {
			model += " " + d.ModelNumber
		}
		hs = append(hs, hint(FieldModel, model, 80))
	}
	if name := strings.TrimSpace(d.FriendlyName); name != "" {
		hs = append(hs, hint(FieldName, name, 70))
	}
	switch deviceTypeShort(d.DeviceType) {
	case "InternetGatewayDevice":
		hs = append(hs, hint(FieldKind, "router", 80))
	case "WLANAccessPointDevice":
		hs = append(hs, hint(FieldKind, "router", 60))
	case "MediaRenderer":
		// Only a suggestion: PCs and phones render media too.
		hs = append(hs, hint(FieldIcon, "tv", 40), hint(FieldTag, "media", 60))
	case "MediaServer":
		hs = append(hs, hint(FieldTag, "media", 50))
	case "Printer":
		hs = append(hs, hint(FieldKind, "printer", 80))
	case "DigitalSecurityCamera":
		hs = append(hs, hint(FieldIcon, "cctv", 70), hint(FieldTag, "camera", 60))
	case "ZonePlayer":
		hs = append(hs, hint(FieldIcon, "speaker", 70), hint(FieldTag, "media", 60))
	}
	if strings.Contains(d.DeviceType, "roku-com") {
		hs = append(hs, hint(FieldIcon, "tv", 70), hint(FieldVendor, "Roku", 70), hint(FieldTag, "media", 60))
	}
	return hs
}

// ssdpHints turns every device of one address into hints, each tagged with
// evidence naming its deviceType and friendly name.
func ssdpHints(devs []probe.UPnPDevice) []store.Hint {
	var out []store.Hint
	for _, d := range devs {
		detail := fmt.Sprintf("UPnP %s %q", deviceTypeShort(d.DeviceType), strings.TrimSpace(d.FriendlyName))
		out = append(out, finish(deviceHints(d), detail)...)
	}
	return dedupe(out)
}
