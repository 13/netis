package probe

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const descXML = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0"><device>
<deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
<friendlyName> Kitchen speaker </friendlyName>
<manufacturer>Sonos, Inc.</manufacturer>
<modelName>Sonos One</modelName>
<modelNumber>S18</modelNumber>
</device></root>`

// ssdpResponder answers any M-SEARCH with one response per location.
func ssdpResponder(t *testing.T, locations ...string) string {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(buf[:n])))
			if err != nil || req.Method != "M-SEARCH" {
				continue
			}
			for _, loc := range locations {
				resp := fmt.Sprintf("HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=1800\r\nST: upnp:rootdevice\r\nLOCATION: %s\r\n\r\n", loc)
				c.WriteToUDP([]byte(resp), from)
			}
		}
	}()
	return c.LocalAddr().String()
}

func TestSearchSSDPFetchesDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, descXML)
	}))
	defer srv.Close()
	addr := ssdpResponder(t, srv.URL+"/desc.xml")
	got, err := searchSSDP(context.Background(), nil, addr, 300*time.Millisecond, ssdpClient())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("devices = %+v", got)
	}
	d := got[0]
	if d.IP != "127.0.0.1" || d.FriendlyName != "Kitchen speaker" || d.Manufacturer != "Sonos, Inc." ||
		d.ModelName != "Sonos One" || d.ModelNumber != "S18" || !strings.HasSuffix(d.DeviceType, "MediaRenderer:1") {
		t.Fatalf("device = %+v", d)
	}
}

func TestSearchSSDPOnlyFetchesFromResponder(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, descXML)
	}))
	defer srv.Close()
	// Same server, but named "localhost": not the responder's address.
	other := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	addr := ssdpResponder(t, other+"/desc.xml", "file:///etc/passwd")
	got, _ := searchSSDP(context.Background(), nil, addr, 300*time.Millisecond, ssdpClient())
	if len(got) != 0 || hits.Load() != 0 {
		t.Fatalf("fetched from elsewhere: devices=%+v hits=%d", got, hits.Load())
	}
}

func TestSearchSSDPCapsBodyAndIgnoresRedirects(t *testing.T) {
	big := strings.Repeat("x", 70<<10)
	mux := http.NewServeMux()
	mux.HandleFunc("/big.xml", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<root><device><friendlyName>`+big+`</friendlyName></device></root>`)
	})
	mux.HandleFunc("/moved.xml", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/desc.xml", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	addr := ssdpResponder(t, srv.URL+"/big.xml", srv.URL+"/moved.xml")
	got, _ := searchSSDP(context.Background(), nil, addr, 300*time.Millisecond, ssdpClient())
	if len(got) != 0 {
		t.Fatalf("devices = %+v", got)
	}
}

func TestFetchAllowed(t *testing.T) {
	for _, c := range []struct {
		loc, ip string
		want    bool
	}{
		{"http://192.168.1.5:1400/desc.xml", "192.168.1.5", true},
		{"https://192.168.1.5/desc.xml", "192.168.1.5", true},
		{"http://192.168.1.6/desc.xml", "192.168.1.5", false},
		{"http://localhost/desc.xml", "127.0.0.1", false},
		{"http://192.168.1.5.evil.example/", "192.168.1.5", false},
		{"http://user@192.168.1.5@10.0.0.1/", "192.168.1.5", false},
		{"ftp://192.168.1.5/desc.xml", "192.168.1.5", false},
		{"file:///etc/passwd", "192.168.1.5", false},
		{"/desc.xml", "192.168.1.5", false},
		{"http://%zz/", "192.168.1.5", false},
	} {
		if got := fetchAllowed(c.loc, c.ip); got != c.want {
			t.Errorf("fetchAllowed(%q, %q) = %v, want %v", c.loc, c.ip, got, c.want)
		}
	}
}

// The redirect target serves a valid description, so following it would
// yield a device.
func TestSearchSSDPDoesNotFollowRedirectToValidDescription(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/desc.xml", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, descXML) })
	mux.HandleFunc("/moved.xml", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/desc.xml", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	addr := ssdpResponder(t, srv.URL+"/moved.xml")
	got, _ := searchSSDP(context.Background(), nil, addr, 300*time.Millisecond, ssdpClient())
	if len(got) != 0 {
		t.Fatalf("devices = %+v", got)
	}
}
