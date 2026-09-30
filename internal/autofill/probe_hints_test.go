package autofill

import (
	"strings"
	"testing"

	"netis/internal/probe"
)

type want struct {
	field, value string
	conf         int
}

// TestMDNSHints exercises every row of the mDNS hint table.
func TestMDNSHints(t *testing.T) {
	cases := []struct {
		name  string
		svc   probe.MDNSService
		wants []want
	}{
		{
			name: "googlecast",
			svc: probe.MDNSService{
				Type: "_googlecast._tcp", Instance: "Living Room TV",
				TXT: map[string]string{"md": "Chromecast", "fn": "Living Room TV"},
			},
			wants: []want{
				{"model", "Chromecast", 80},
				{"name", "Living Room TV", 70},
				{"icon", "tv", 60},
				{"tag", "media", 60},
			},
		},
		{
			name: "googlecast empty TXT falls back to instance",
			svc: probe.MDNSService{
				Type: "_googlecast._tcp", Instance: "Family Room",
				TXT: map[string]string{"md": "", "fn": ""},
			},
			wants: []want{
				{"name", "Family Room", 70},
				{"icon", "tv", 60},
				{"tag", "media", 60},
			},
		},
		{
			name: "airplay AppleTV",
			svc: probe.MDNSService{
				Type: "_airplay._tcp", Instance: "Bedroom Apple TV",
				TXT: map[string]string{"model": "AppleTV6,2"},
			},
			wants: []want{
				{"model", "Apple TV (AppleTV6,2)", 70},
				{"vendor", "Apple", 70},
				{"icon", "tv", 70},
				{"name", "Bedroom Apple TV", 60},
			},
		},
		{
			name: "airplay HomePod",
			svc: probe.MDNSService{
				Type: "_airplay._tcp", Instance: "Kitchen",
				TXT: map[string]string{"model": "AudioAccessory5,1"},
			},
			wants: []want{
				{"model", "HomePod (AudioAccessory5,1)", 70},
				{"vendor", "Apple", 70},
				{"icon", "speaker", 70},
			},
		},
		{
			name: "raop",
			svc: probe.MDNSService{
				Type: "_raop._tcp", Instance: "A1B2C3D4E5F6@Kitchen",
				TXT: map[string]string{"am": "AudioAccessory5,1"},
			},
			wants: []want{
				{"model", "HomePod (AudioAccessory5,1)", 60},
				{"name", "Kitchen", 50},
			},
		},
		{
			name: "device-info MacBookPro",
			svc: probe.MDNSService{
				Type: "_device-info._tcp", Instance: "Bens-MacBook-Pro",
				TXT: map[string]string{"model": "MacBookPro18,3"},
			},
			wants: []want{
				{"model", "MacBook Pro (MacBookPro18,3)", 70},
				{"vendor", "Apple", 70},
				{"kind", "computer", 60},
			},
		},
		{
			name: "companion-link iPad",
			svc: probe.MDNSService{
				Type: "_companion-link._tcp", Instance: "Bens-iPad",
				TXT: map[string]string{"rpmd": "iPad13,4"},
			},
			wants: []want{
				{"vendor", "Apple", 70},
				{"model", "iPad (iPad13,4)", 70},
				{"kind", "phone", 70},
				{"icon", "tablet", 70},
			},
		},
		{
			name: "ipp printer, usb_mdl beats ty, vendor normalised",
			svc: probe.MDNSService{
				Type: "_ipp._tcp", Instance: "Brother HL-L2340D",
				TXT: map[string]string{
					"usb_mdl": "HL-L2340D series",
					"ty":      "Brother HL-L2340D series",
					"usb_mfg": "Brother Industries, Ltd.",
				},
			},
			wants: []want{
				{"kind", "printer", 90},
				{"model", "HL-L2340D series", 80},
				{"vendor", "Brother", 80},
				{"name", "Brother HL-L2340D", 60},
			},
		},
		{
			name: "printer falls back to ty when usb_mdl absent",
			svc: probe.MDNSService{
				Type: "_ipps._tcp", Instance: "Office Printer",
				TXT: map[string]string{"ty": "Brother HL-L2340D series"},
			},
			wants: []want{
				{"kind", "printer", 90},
				{"model", "Brother HL-L2340D series", 75},
			},
		},
		{
			name:  "printer type wired for _printer._tcp",
			svc:   probe.MDNSService{Type: "_printer._tcp", Instance: "P1"},
			wants: []want{{"kind", "printer", 90}},
		},
		{
			name:  "printer type wired for _pdl-datastream._tcp",
			svc:   probe.MDNSService{Type: "_pdl-datastream._tcp", Instance: "P2"},
			wants: []want{{"kind", "printer", 90}},
		},
		{
			name: "hap ci=5 lightbulb",
			svc: probe.MDNSService{
				Type: "_hap._tcp", Instance: "Hallway Bulb",
				TXT: map[string]string{"md": "LIFX A19", "ci": "5"},
			},
			wants: []want{
				{"kind", "iot", 70},
				{"model", "LIFX A19", 70},
				{"icon", "lightbulb", 70},
				{"tag", "smart-home", 60},
				{"name", "Hallway Bulb", 60},
			},
		},
		{
			name: "hue",
			svc:  probe.MDNSService{Type: "_hue._tcp", Instance: "Philips-hue"},
			wants: []want{
				{"vendor", "Philips Hue", 80},
				{"kind", "iot", 80},
				{"model", "Hue Bridge", 70},
				{"tag", "smart-home", 60},
			},
		},
		{
			name: "esphomelib",
			svc: probe.MDNSService{
				Type: "_esphomelib._tcp", Instance: "esp32-livingroom",
				TXT: map[string]string{"friendly_name": "Living Room Sensor"},
			},
			wants: []want{
				{"kind", "iot", 80},
				{"function", "ESPHome", 70},
				{"tag", "smart-home", 60},
				{"name", "Living Room Sensor", 60},
			},
		},
		{
			name: "home-assistant",
			svc:  probe.MDNSService{Type: "_home-assistant._tcp", Instance: "homeassistant"},
			wants: []want{
				{"kind", "server", 60},
				{"function", "Home Assistant", 90},
				{"tag", "smart-home", 60},
			},
		},
		{
			name:  "smb",
			svc:   probe.MDNSService{Type: "_smb._tcp", Instance: "fileserver"},
			wants: []want{{"tag", "file-share", 50}},
		},
		{
			name: "sonos",
			svc:  probe.MDNSService{Type: "_sonos._tcp", Instance: "Kitchen"},
			wants: []want{
				{"vendor", "Sonos", 90},
				{"icon", "speaker", 80},
				{"tag", "media", 60},
			},
		},
		{
			name:  "spotify-connect",
			svc:   probe.MDNSService{Type: "_spotify-connect._tcp", Instance: "Kitchen Speaker"},
			wants: []want{{"tag", "media", 50}},
		},
		{
			name: "workstation strips MAC suffix",
			svc:  probe.MDNSService{Type: "_workstation._tcp", Instance: "nas [aa:bb:cc:dd:ee:ff]"},
			wants: []want{
				{"kind", "computer", 60},
				{"name", "nas", 50},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hs := mdnsHints([]probe.MDNSService{c.svc})
			for _, w := range c.wants {
				if !has(hs, w.field, w.value, w.conf) {
					t.Errorf("missing %+v in %+v", w, hs)
				}
			}
		})
	}
}

func TestMDNSHintsDetail(t *testing.T) {
	hs := mdnsHints([]probe.MDNSService{{
		Type: "_googlecast._tcp", Instance: "Living Room TV",
		TXT: map[string]string{"md": "Chromecast", "fn": "Living Room TV"},
	}})
	want := `mDNS _googlecast._tcp "Living Room TV"`
	for _, h := range hs {
		if h.Detail != want {
			t.Errorf("detail = %q, want %q", h.Detail, want)
		}
	}
}

func TestMDNSHintsNoHandlerForUnknownType(t *testing.T) {
	hs := mdnsHints([]probe.MDNSService{{Type: "_unknown._tcp", Instance: "x"}})
	if len(hs) != 0 {
		t.Errorf("hints = %+v, want none", hs)
	}
}

// TestSSDPHints exercises every row of the UPnP hint table.
func TestSSDPHints(t *testing.T) {
	cases := []struct {
		name  string
		dev   probe.UPnPDevice
		wants []want
	}{
		{
			name: "Sonos MediaRenderer description",
			dev: probe.UPnPDevice{
				DeviceType:   "urn:schemas-upnp-org:device:MediaRenderer:1",
				Manufacturer: "Sonos, Inc.", ModelName: "Sonos One", ModelNumber: "S18",
				FriendlyName: "Kitchen",
			},
			wants: []want{
				{"vendor", "Sonos", 80},
				{"model", "Sonos One S18", 80},
				{"name", "Kitchen", 70},
				{"icon", "tv", 40},
				{"tag", "media", 60},
			},
		},
		{
			name: "ZonePlayer",
			dev: probe.UPnPDevice{
				DeviceType:   "urn:schemas-upnp-org:device:ZonePlayer:1",
				Manufacturer: "Sonos, Inc.", ModelName: "Sonos One", FriendlyName: "Kitchen",
			},
			wants: []want{
				{"icon", "speaker", 70},
				{"tag", "media", 60},
			},
		},
		{
			name: "InternetGatewayDevice",
			dev: probe.UPnPDevice{
				DeviceType:   "urn:schemas-upnp-org:device:InternetGatewayDevice:1",
				Manufacturer: "NETGEAR, Inc.", ModelName: "Nighthawk", FriendlyName: "Router",
			},
			wants: []want{{"kind", "router", 80}},
		},
		{
			name: "WLANAccessPointDevice",
			dev: probe.UPnPDevice{
				DeviceType: "urn:schemas-upnp-org:device:WLANAccessPointDevice:1",
			},
			wants: []want{{"kind", "router", 60}},
		},
		{
			name:  "MediaServer",
			dev:   probe.UPnPDevice{DeviceType: "urn:schemas-upnp-org:device:MediaServer:1"},
			wants: []want{{"tag", "media", 50}},
		},
		{
			name:  "Printer",
			dev:   probe.UPnPDevice{DeviceType: "urn:schemas-upnp-org:device:Printer:1"},
			wants: []want{{"kind", "printer", 80}},
		},
		{
			name: "DigitalSecurityCamera",
			dev:  probe.UPnPDevice{DeviceType: "urn:schemas-upnp-org:device:DigitalSecurityCamera:1"},
			wants: []want{
				{"icon", "cctv", 70},
				{"tag", "camera", 60},
			},
		},
		{
			name: "roku-com",
			dev:  probe.UPnPDevice{DeviceType: "urn:roku-com:device:player:1"},
			wants: []want{
				{"icon", "tv", 70},
				{"vendor", "Roku", 70},
				{"tag", "media", 60},
			},
		},
		{
			name: "model number already contained is not duplicated",
			dev: probe.UPnPDevice{
				DeviceType: "urn:schemas-upnp-org:device:Basic:1",
				ModelName:  "Sonos One SL", ModelNumber: "SL",
			},
			wants: []want{{"model", "Sonos One SL", 80}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hs := ssdpHints([]probe.UPnPDevice{c.dev})
			for _, w := range c.wants {
				if !has(hs, w.field, w.value, w.conf) {
					t.Errorf("missing %+v in %+v", w, hs)
				}
			}
		})
	}
}

func TestSSDPHintsBasicAddsNothingExtra(t *testing.T) {
	hs := ssdpHints([]probe.UPnPDevice{{
		DeviceType:   "urn:schemas-upnp-org:device:Basic:1",
		Manufacturer: "Acme", ModelName: "Widget", FriendlyName: "Thing",
	}})
	for _, h := range hs {
		switch h.Field {
		case "kind", "icon", "tag":
			t.Errorf("Basic device type should add no kind/icon/tag hints, got %+v", h)
		}
	}
	if !has(hs, "vendor", "Acme", 80) || !has(hs, "model", "Widget", 80) || !has(hs, "name", "Thing", 70) {
		t.Errorf("hints = %+v", hs)
	}
}

func TestSSDPHintsDetail(t *testing.T) {
	hs := ssdpHints([]probe.UPnPDevice{{
		DeviceType:   "urn:schemas-upnp-org:device:MediaRenderer:1",
		FriendlyName: "Kitchen",
	}})
	want := `UPnP MediaRenderer "Kitchen"`
	for _, h := range hs {
		if h.Detail != want {
			t.Errorf("detail = %q, want %q", h.Detail, want)
		}
	}
}

func TestAppleModel(t *testing.T) {
	cases := map[string]string{
		"MacBookPro18,3": "MacBook Pro (MacBookPro18,3)",
		"Macmini9,1":     "Mac mini (Macmini9,1)",
		"Mac14,2":        "Mac14,2",
		"MacBookProMax":  "MacBookProMax",
		"":               "",
	}
	for in, want := range cases {
		if got := appleModel(in); got != want {
			t.Errorf("appleModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHAPCategoryIcons(t *testing.T) {
	cases := map[string]string{
		"5": "lightbulb", "7": "plug", "8": "plug",
		"9": "thermometer", "10": "thermometer",
		"17": "cctv", "18": "cctv",
		"24": "tv", "31": "tv", "35": "tv", "36": "tv",
		"25": "speaker", "26": "speaker", "34": "speaker",
		"32": "", // Target Controller: a remote, not a TV
		"99": "",
	}
	for ci, icon := range cases {
		t.Run("ci="+ci, func(t *testing.T) {
			hs := mdnsHints([]probe.MDNSService{{
				Type: "_hap._tcp", Instance: "Accessory",
				TXT: map[string]string{"ci": ci},
			}})
			var got []string
			for _, h := range hs {
				if h.Field == "icon" {
					got = append(got, h.Value)
				}
			}
			switch {
			case icon == "" && len(got) != 0:
				t.Errorf("icons = %v, want none", got)
			case icon != "" && !has(hs, "icon", icon, 70):
				t.Errorf("icons = %v, want %q", got, icon)
			}
		})
	}
}

// TestHAPBridgeOnlyTags checks that a HomeKit bridge (ci=2), which runs on a
// server such as Home Assistant or Homebridge, says nothing about the host
// but that it does smart-home work.
func TestHAPBridgeOnlyTags(t *testing.T) {
	hs := mdnsHints([]probe.MDNSService{{
		Type: "_hap._tcp", Instance: "HASS Bridge AB12",
		TXT: map[string]string{"md": "HASS Bridge", "ci": "2"},
	}})
	if len(hs) != 1 || !has(hs, "tag", "smart-home", 60) {
		t.Fatalf("hints = %+v, want only the smart-home tag", hs)
	}
}

func TestGooglecastIcon(t *testing.T) {
	cases := map[string]string{
		"Chromecast":          "tv",
		"Google TV Streamer":  "tv",
		"Nest Mini":           "speaker",
		"Nest Audio":          "speaker",
		"Google Home":         "speaker",
		"Google Home Mini":    "speaker",
		"Chromecast Audio":    "speaker",
		"JBL Link 20 speaker": "speaker",
		"Google Cast Group":   "speaker",
	}
	for md, icon := range cases {
		t.Run(md, func(t *testing.T) {
			hs := mdnsHints([]probe.MDNSService{{
				Type: "_googlecast._tcp", Instance: "x",
				TXT: map[string]string{"md": md},
			}})
			if !has(hs, "icon", icon, 60) {
				t.Errorf("hints = %+v, want icon %q", hs, icon)
			}
			for _, h := range hs {
				if h.Field == "icon" && h.Value != icon {
					t.Errorf("extra icon %+v", h)
				}
			}
		})
	}
}

// TestSSDPMediaSoftware checks that media software running on a
// general-purpose machine does not name the machine's vendor or model.
func TestSSDPMediaSoftware(t *testing.T) {
	cases := []probe.UPnPDevice{
		{DeviceType: "urn:schemas-upnp-org:device:MediaRenderer:1", Manufacturer: "Microsoft Corporation",
			ModelName: "Windows Media Player", ModelNumber: "12", FriendlyName: "DESKTOP-1"},
		{DeviceType: "urn:schemas-upnp-org:device:MediaServer:1", Manufacturer: "Microsoft Corporation",
			ModelName: "Windows Media Player Sharing", FriendlyName: "DESKTOP-1"},
		{DeviceType: "urn:schemas-upnp-org:device:MediaServer:1", Manufacturer: "Plex, Inc.",
			ModelName: "Plex Media Server", FriendlyName: "nas"},
		{DeviceType: "urn:schemas-upnp-org:device:MediaRenderer:1", Manufacturer: "XBMC Foundation",
			ModelName: "Kodi", FriendlyName: "htpc"},
		{DeviceType: "urn:schemas-upnp-org:device:MediaServer:1", Manufacturer: "Justin Maggard",
			ModelName: "MiniDLNA", FriendlyName: "nas"},
		{DeviceType: "urn:schemas-upnp-org:device:MediaServer:1", Manufacturer: "Jellyfin",
			ModelName: "jellyfin", FriendlyName: "media"},
		{DeviceType: "urn:schemas-upnp-org:device:MediaServer:1", Manufacturer: "Petr Nejedly",
			ModelName: "Serviio UPnP/AV Server", FriendlyName: "media"},
		{DeviceType: "urn:schemas-upnp-org:device:MediaServer:1", Manufacturer: "Universal Media Server",
			ModelName: "Universal Media Server", FriendlyName: "media"},
	}
	for _, d := range cases {
		t.Run(d.ModelName, func(t *testing.T) {
			hs := ssdpHints([]probe.UPnPDevice{d})
			for _, h := range hs {
				if h.Field == "model" || h.Field == "vendor" {
					t.Errorf("unexpected %+v", h)
				}
			}
			if !has(hs, "name", d.FriendlyName, 70) {
				t.Errorf("hints = %+v, want the name kept", hs)
			}
		})
	}
	// A real renderer keeps its vendor and model.
	hs := ssdpHints([]probe.UPnPDevice{{DeviceType: "urn:schemas-upnp-org:device:MediaRenderer:1",
		Manufacturer: "Samsung Electronics", ModelName: "UE55TU7000", FriendlyName: "TV"}})
	if !has(hs, "model", "UE55TU7000", 80) || !has(hs, "vendor", "Samsung", 80) {
		t.Errorf("hints = %+v", hs)
	}
}

func TestProbeHintValuesCleaned(t *testing.T) {
	long := strings.Repeat("é", 200)
	hs := mdnsHints([]probe.MDNSService{{
		Type: "_googlecast._tcp", Instance: "x",
		TXT: map[string]string{"md": "Chrome\ncast\t  Ultra\x00 ", "fn": long},
	}})
	if !has(hs, "model", "Chrome cast Ultra", 80) {
		t.Errorf("hints = %+v, want cleaned model", hs)
	}
	if !has(hs, "name", strings.Repeat("é", 128), 70) {
		t.Errorf("hints = %+v, want name capped at 128 runes", hs)
	}
	hs = ssdpHints([]probe.UPnPDevice{{
		DeviceType:   "urn:schemas-upnp-org:device:Basic:1",
		Manufacturer: "Acme", ModelName: "\r\n\t", FriendlyName: "Front\r\nDoor",
	}})
	for _, h := range hs {
		if h.Field == "model" {
			t.Errorf("empty model kept: %+v", h)
		}
	}
	if !has(hs, "name", "Front Door", 70) {
		t.Errorf("hints = %+v, want cleaned name", hs)
	}
	// Bidi overrides and zero-width characters could make a name display as
	// something else.
	hs = ssdpHints([]probe.UPnPDevice{{
		DeviceType:   "urn:schemas-upnp-org:device:Basic:1",
		FriendlyName: "Ki\u200btchen \u202eVT",
	}})
	if !has(hs, "name", "Kitchen VT", 70) {
		t.Errorf("hints = %+v, want format characters dropped", hs)
	}
}
