// Package opnsense reads DHCP leases and the ARP table from an OPNsense
// firewall's API.
package opnsense

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"netis/internal/leases"
)

// Leases is what one DHCP backend on the firewall reported.
type Leases struct {
	// Backend names the DHCP server that answered: "kea", "isc" or
	// "dnsmasq".
	Backend string
	Static  []leases.Entry
	Dynamic []leases.Entry
}

// ARPEntry is one row of the firewall's ARP table.
type ARPEntry struct {
	IP      string
	MAC     string
	Expired bool
}

// Client talks to the OPNsense API with an API key and secret as HTTP basic
// auth.
type Client struct {
	base   string
	key    string
	secret string
	http   *http.Client
}

func NewClient(baseURL, key, secret string, insecure bool) *Client {
	tr := &http.Transport{}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		base:   strings.TrimSuffix(baseURL, "/"),
		key:    key,
		secret: secret,
		http:   &http.Client{Transport: tr, Timeout: 10 * time.Second},
	}
}

// Close releases the client's idle connections.
func (c *Client) Close() { c.http.CloseIdleConnections() }

// httpError is a non-200 answer. Its message keeps the "HTTP <code>" shape
// the runner's failure categories look for.
type httpError struct {
	path   string
	status int
}

func (e *httpError) Error() string { return fmt.Sprintf("opnsense %s: HTTP %d", e.path, e.status) }

// skippable reports whether err means this backend is not there for us: not
// installed (404) or not granted to the API key (403).
func skippable(err error) bool {
	var he *httpError
	return errors.As(err, &he) && (he.status == http.StatusNotFound || he.status == http.StatusForbidden)
}

// get fetches path and decodes the JSON answer into out.
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.key, c.secret)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	p, _, _ := strings.Cut(path, "?")
	if resp.StatusCode != 200 {
		io.Copy(io.Discard, resp.Body) // drain so the connection can be reused
		return &httpError{path: p, status: resp.StatusCode}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("opnsense %s: %w", p, err)
	}
	return nil
}

// flexString decodes a JSON string, number or boolean as its text; OPNsense
// is not consistent about which it sends.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	if string(b) == "null" {
		*f = ""
		return nil
	}
	*f = flexString(b)
	return nil
}

// allRows asks a search endpoint for every row in one page.
const allRows = "?current=1&rowCount=-1"

// Leases returns the leases of the first DHCP backend that has any, trying
// Kea, then ISC, then Dnsmasq. Kea's API exists on every current install even
// when ISC serves DHCP, so an empty answer moves on to the next backend just
// like a missing one. A backend that is not installed (404) or not granted to
// the key (403) is skipped; when every backend is skipped the first such error
// is returned. When backends answer but none has leases, the result is empty.
func (c *Client) Leases(ctx context.Context) (Leases, error) {
	backends := []struct {
		name  string
		fetch func(context.Context) (Leases, error)
	}{
		{"kea", c.keaLeases},
		{"isc", c.iscLeases},
		{"dnsmasq", c.dnsmasqLeases},
	}
	var firstSkip error
	answered := ""
	for _, b := range backends {
		l, err := b.fetch(ctx)
		if skippable(err) {
			if firstSkip == nil {
				firstSkip = err
			}
			continue
		}
		if err != nil {
			return Leases{}, err
		}
		if len(l.Static)+len(l.Dynamic) > 0 {
			l.Backend = b.name
			return l, nil
		}
		if answered == "" {
			answered = b.name
		}
	}
	if answered != "" {
		return Leases{Backend: answered}, nil
	}
	return Leases{}, firstSkip
}

func entry(mac, ip string, hostname flexString) (leases.Entry, bool) {
	m, a := leases.NormMAC(mac), leases.NormIP(ip)
	if m == "" || a == "" {
		return leases.Entry{}, false
	}
	return leases.Entry{MAC: m, IP: a, Hostname: strings.TrimSpace(string(hostname))}, true
}

// keaLeases reads Kea's lease file (leases4/search) and its reservations.
// Kea lease states other than 0 (declined, expired-reclaimed) are not held.
func (c *Client) keaLeases(ctx context.Context) (Leases, error) {
	var body struct {
		Rows []struct {
			Address   string     `json:"address"`
			HWAddr    string     `json:"hwaddr"`
			HWAddress string     `json:"hw_address"`
			Hostname  flexString `json:"hostname"`
			State     flexString `json:"state"`
		} `json:"rows"`
	}
	if err := c.get(ctx, "/api/kea/leases4/search"+allRows, &body); err != nil {
		return Leases{}, err
	}
	var out Leases
	for _, r := range body.Rows {
		if r.State != "" && r.State != "0" {
			continue
		}
		mac := r.HWAddr
		if mac == "" {
			mac = r.HWAddress
		}
		if e, ok := entry(mac, r.Address, r.Hostname); ok {
			out.Dynamic = append(out.Dynamic, e)
		}
	}

	var res struct {
		Rows []struct {
			HWAddress string     `json:"hw_address"`
			IPAddress string     `json:"ip_address"`
			Hostname  flexString `json:"hostname"`
		} `json:"rows"`
	}
	err := c.get(ctx, "/api/kea/dhcpv4/searchReservation"+allRows, &res)
	if skippable(err) {
		return out, nil // leases without reservations are still worth having
	}
	if err != nil {
		return Leases{}, err
	}
	for _, r := range res.Rows {
		if e, ok := entry(r.HWAddress, r.IPAddress, r.Hostname); ok {
			out.Static = append(out.Static, e)
		}
	}
	return out, nil
}

// iscLeases reads ISC dhcpd's leases, where static mappings are rows of type
// "static". A dynamic row whose state is given and not "active" is a lease
// that has ended.
func (c *Client) iscLeases(ctx context.Context) (Leases, error) {
	var body struct {
		Rows []struct {
			Address  string     `json:"address"`
			MAC      string     `json:"mac"`
			Hostname flexString `json:"hostname"`
			Type     flexString `json:"type"`
			State    flexString `json:"state"`
		} `json:"rows"`
	}
	if err := c.get(ctx, "/api/dhcpv4/leases/searchLease"+allRows, &body); err != nil {
		return Leases{}, err
	}
	var out Leases
	for _, r := range body.Rows {
		e, ok := entry(r.MAC, r.Address, r.Hostname)
		if !ok {
			continue
		}
		if r.Type == "static" {
			out.Static = append(out.Static, e)
			continue
		}
		if r.State != "" && r.State != "active" {
			continue
		}
		out.Dynamic = append(out.Dynamic, e)
	}
	return out, nil
}

// dnsmasqLeases reads Dnsmasq's leases (OPNsense 25.1 and later).
func (c *Client) dnsmasqLeases(ctx context.Context) (Leases, error) {
	var body struct {
		Rows []struct {
			Address  string     `json:"address"`
			HWAddr   string     `json:"hwaddr"`
			Hostname flexString `json:"hostname"`
		} `json:"rows"`
	}
	if err := c.get(ctx, "/api/dnsmasq/leases/search"+allRows, &body); err != nil {
		return Leases{}, err
	}
	var out Leases
	for _, r := range body.Rows {
		if e, ok := entry(r.HWAddr, r.Address, r.Hostname); ok {
			out.Dynamic = append(out.Dynamic, e)
		}
	}
	return out, nil
}

// Pools returns the DHCP pools of backend, the one that served the leases:
// Kea's subnet pools or Dnsmasq's ranges. ISC dhcpd has no API for its
// ranges, so for it (and for no backend) the answer is none.
func (c *Client) Pools(ctx context.Context, backend string) ([]leases.Range, error) {
	var out []leases.Range
	switch backend {
	case "kea":
		var body struct {
			Rows []struct {
				Pools flexString `json:"pools"`
			} `json:"rows"`
		}
		if err := c.get(ctx, "/api/kea/dhcpv4/searchSubnet"+allRows, &body); err != nil {
			return nil, err
		}
		// A subnet's pools are one per line, "first-last" or a CIDR block;
		// commas are accepted too.
		for _, row := range body.Rows {
			for _, p := range strings.FieldsFunc(string(row.Pools), func(r rune) bool { return r == '\n' || r == ',' }) {
				if r, ok := leases.ParseRange(p); ok {
					out = append(out, r)
				}
			}
		}
	case "dnsmasq":
		var body struct {
			Rows []struct {
				Start flexString `json:"start_addr"`
				End   flexString `json:"end_addr"`
			} `json:"rows"`
		}
		if err := c.get(ctx, "/api/dnsmasq/settings/searchRange"+allRows, &body); err != nil {
			return nil, err
		}
		for _, row := range body.Rows {
			if r, ok := leases.ParseRange(string(row.Start) + "-" + string(row.End)); ok {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// ARP returns the firewall's ARP table. getArp answers with a bare array;
// an object with rows, as the newer search endpoints use, is accepted too.
// Rows without a valid IP and MAC are dropped.
func (c *Client) ARP(ctx context.Context) ([]ARPEntry, error) {
	type row struct {
		IP      string     `json:"ip"`
		MAC     string     `json:"mac"`
		Expired flexString `json:"expired"`
	}
	var raw json.RawMessage
	if err := c.get(ctx, "/api/diagnostics/interface/getArp", &raw); err != nil {
		return nil, err
	}
	var rows []row
	if t := bytes.TrimSpace(raw); len(t) > 0 && t[0] == '{' {
		var wrapped struct {
			Rows []row `json:"rows"`
		}
		if err := json.Unmarshal(t, &wrapped); err != nil {
			return nil, fmt.Errorf("opnsense getArp: %w", err)
		}
		rows = wrapped.Rows
	} else if err := json.Unmarshal(t, &rows); err != nil {
		return nil, fmt.Errorf("opnsense getArp: %w", err)
	}
	out := make([]ARPEntry, 0, len(rows))
	for _, r := range rows {
		ip, mac := leases.NormIP(r.IP), leases.NormMAC(r.MAC)
		if ip == "" || mac == "" {
			continue
		}
		expired := r.Expired == "true" || r.Expired == "1"
		out = append(out, ARPEntry{IP: ip, MAC: mac, Expired: expired})
	}
	return out, nil
}
