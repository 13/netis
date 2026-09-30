package probe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

// ssdpAddr is the IPv4 SSDP group and port.
const ssdpAddr = "239.255.255.250:1900"

// ssdpSearch asks every UPnP device on the link to answer.
const ssdpSearch = "M-SEARCH * HTTP/1.1\r\n" +
	"HOST: 239.255.255.250:1900\r\n" +
	"MAN: \"ssdp:discover\"\r\n" +
	"MX: 2\r\n" +
	"ST: ssdp:all\r\n" +
	"\r\n"

const (
	maxLocationsPerIP = 4        // description URLs kept per responder
	maxFetches        = 8        // descriptions fetched at once
	maxDescription    = 64 << 10 // bytes of a description read
)

// UPnPDevice is a root device a host described over UPnP.
type UPnPDevice struct {
	IP           string // responder address
	Location     string
	DeviceType   string // e.g. "urn:schemas-upnp-org:device:MediaRenderer:1"
	FriendlyName string
	Manufacturer string
	ModelName    string
	ModelNumber  string
}

// SearchSSDP sends an M-SEARCH out of ifi and returns the root devices whose
// descriptions could be fetched, one per (IP, Location).
func SearchSSDP(ctx context.Context, ifi *net.Interface, window time.Duration) ([]UPnPDevice, error) {
	return searchSSDP(ctx, ifi, ssdpAddr, window, ssdpClient())
}

// ssdpClient fetches descriptions: short timeout, redirects not followed.
func ssdpClient() *http.Client {
	return &http.Client{
		Timeout:       2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		// A description is on the LAN, next to the responder: never send it
		// through an HTTP_PROXY, and keep no idle connections to devices.
		Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
	}
}

// fetchAllowed reports whether a description may be fetched from location
// advertised by srcIP: an http or https URL whose host is srcIP itself.
// This keeps an SSDP answer from pointing the fetch at any other address.
func fetchAllowed(location, srcIP string) bool {
	u, err := url.Parse(location)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Hostname() != "" && u.Hostname() == srcIP
}

type upnpRoot struct {
	Device struct {
		DeviceType   string `xml:"deviceType"`
		FriendlyName string `xml:"friendlyName"`
		Manufacturer string `xml:"manufacturer"`
		ModelName    string `xml:"modelName"`
		ModelNumber  string `xml:"modelNumber"`
	} `xml:"device"`
}

// ssdpLoc is one advertised description URL and the address that sent it.
type ssdpLoc struct{ ip, location string }

// ssdpLocation returns the LOCATION header of an SSDP response packet.
func ssdpLocation(pkt []byte) string {
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(pkt)), nil)
	if err != nil {
		return ""
	}
	resp.Body.Close()
	return strings.TrimSpace(resp.Header.Get("Location"))
}

// fetchDescription fetches and decodes the description at l, returning false
// when it is not allowed, fails, or names nothing useful.
func fetchDescription(ctx context.Context, client *http.Client, l ssdpLoc) (UPnPDevice, bool) {
	if !fetchAllowed(l.location, l.ip) {
		return UPnPDevice{}, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.location, nil)
	if err != nil {
		return UPnPDevice{}, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return UPnPDevice{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UPnPDevice{}, false
	}
	var root upnpRoot
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxDescription)).Decode(&root); err != nil {
		return UPnPDevice{}, false
	}
	d := UPnPDevice{
		IP:           l.ip,
		Location:     l.location,
		DeviceType:   strings.TrimSpace(root.Device.DeviceType),
		FriendlyName: strings.TrimSpace(root.Device.FriendlyName),
		Manufacturer: strings.TrimSpace(root.Device.Manufacturer),
		ModelName:    strings.TrimSpace(root.Device.ModelName),
		ModelNumber:  strings.TrimSpace(root.Device.ModelNumber),
	}
	if d.DeviceType == "" && d.Manufacturer == "" && d.ModelName == "" && d.FriendlyName == "" {
		return UPnPDevice{}, false
	}
	return d, true
}

// searchSSDP sends the M-SEARCH to addr (the SSDP group in production, a
// local responder in tests), collects LOCATIONs until window passes or ctx
// ends, then fetches each allowed description with client.
func searchSSDP(ctx context.Context, ifi *net.Interface, addr string, window time.Duration, client *http.Client) ([]UPnPDevice, error) {
	dst, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if dst.IP.IsMulticast() {
		pc := ipv4.NewPacketConn(conn)
		if ifi != nil {
			if err := pc.SetMulticastInterface(ifi); err != nil {
				return nil, err
			}
		}
		_ = pc.SetMulticastTTL(2)
	}
	deadline := time.Now().Add(window)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Now()) })
	defer stop()

	if _, err := conn.WriteToUDP([]byte(ssdpSearch), dst); err != nil {
		return nil, err
	}
	// Send again shortly after in case the first packet was lost. Errors on
	// the repeat are ignored: the first send already succeeded.
	resend := time.AfterFunc(100*time.Millisecond, func() {
		_, _ = conn.WriteToUDP([]byte(ssdpSearch), dst)
	})
	defer resend.Stop()

	seen := map[ssdpLoc]bool{}
	perIP := map[string]int{}
	var locs []ssdpLoc
	buf := make([]byte, 9000)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				break
			}
			return nil, err
		}
		ip := from.IP.String()
		loc := ssdpLocation(buf[:n])
		if loc == "" {
			continue
		}
		k := ssdpLoc{ip, loc}
		if seen[k] || perIP[ip] >= maxLocationsPerIP {
			continue
		}
		seen[k] = true
		perIP[ip]++
		locs = append(locs, k)
	}

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []UPnPDevice
		sem = make(chan struct{}, maxFetches)
	)
	for _, l := range locs {
		wg.Add(1)
		sem <- struct{}{}
		go func(l ssdpLoc) {
			defer wg.Done()
			defer func() { <-sem }()
			if d, ok := fetchDescription(ctx, client, l); ok {
				mu.Lock()
				out = append(out, d)
				mu.Unlock()
			}
		}(l)
	}
	wg.Wait()

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.IP != b.IP {
			ai, aerr := netip.ParseAddr(a.IP)
			bi, berr := netip.ParseAddr(b.IP)
			if aerr == nil && berr == nil {
				return ai.Less(bi)
			}
			return a.IP < b.IP
		}
		return a.Location < b.Location
	})
	return out, nil
}
