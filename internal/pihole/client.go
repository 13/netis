package pihole

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type Lease struct {
	IP       string
	MAC      string
	Hostname string
	Expiry   int64
}

type Reservation struct {
	MAC      string
	IP       string
	Hostname string
}

type DNSRecord struct {
	IP   string
	Name string
}

type Client struct {
	base     string
	password string
	http     *http.Client

	mu  sync.Mutex
	sid string
}

func NewClient(baseURL, password string, insecure bool) *Client {
	tr := &http.Transport{}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		base:     strings.TrimSuffix(baseURL, "/"),
		password: password,
		http:     &http.Client{Transport: tr, Timeout: 10 * time.Second},
	}
}

// login authenticates and caches the session id.
func (c *Client) login(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{"password": c.password})
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/api/auth", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("pihole auth: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Session struct {
			SID   string `json:"sid"`
			Valid bool   `json:"valid"`
		} `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if !out.Session.Valid || out.Session.SID == "" {
		return fmt.Errorf("pihole auth: invalid session")
	}
	c.mu.Lock()
	c.sid = out.Session.SID
	c.mu.Unlock()
	return nil
}

// get performs an authenticated GET, decoding JSON into out. It logs in when
// no SID is cached, and re-authenticates once on a 401 before retrying.
func (c *Client) get(ctx context.Context, path string, out any) error {
	c.mu.Lock()
	sid := c.sid
	c.mu.Unlock()
	if sid == "" {
		if err := c.login(ctx); err != nil {
			return err
		}
	}
	status, err := c.doGet(ctx, path, out)
	if err != nil {
		return err
	}
	if status == 401 {
		if err := c.login(ctx); err != nil {
			return err
		}
		status, err = c.doGet(ctx, path, out)
		if err != nil {
			return err
		}
	}
	if status != 200 {
		return fmt.Errorf("pihole %s: HTTP %d", path, status)
	}
	return nil
}

// doGet issues one GET with the cached SID and, on 200, decodes into out.
// It returns the HTTP status so get() can decide whether to re-auth.
func (c *Client) doGet(ctx context.Context, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	req.Header.Set("X-FTL-SID", c.sid)
	c.mu.Unlock()
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return 0, err
	}
	return 200, nil
}

func normMAC(s string) string {
	m := strings.ToLower(strings.TrimSpace(s))
	if _, err := netip.ParseAddr(m); err == nil { // guard against an IP slipping in
		return ""
	}
	if len(m) != 17 {
		return ""
	}
	return m
}

func normIP(s string) string {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return ""
	}
	return a.String()
}

func (c *Client) Leases(ctx context.Context) ([]Lease, error) {
	var body struct {
		Leases []struct {
			IP      string `json:"ip"`
			HWAddr  string `json:"hwaddr"`
			Name    string `json:"name"`
			Expires int64  `json:"expires"`
		} `json:"leases"`
	}
	if err := c.get(ctx, "/api/dhcp/leases", &body); err != nil {
		return nil, err
	}
	out := make([]Lease, 0, len(body.Leases))
	for _, l := range body.Leases {
		ip, mac := normIP(l.IP), normMAC(l.HWAddr)
		if ip == "" || mac == "" {
			continue
		}
		out = append(out, Lease{IP: ip, MAC: mac, Hostname: strings.TrimSpace(l.Name), Expiry: l.Expires})
	}
	return out, nil
}

func (c *Client) Reservations(ctx context.Context) ([]Reservation, error) {
	var body struct {
		Config struct {
			DHCP struct {
				Hosts []string `json:"hosts"`
			} `json:"dhcp"`
		} `json:"config"`
	}
	if err := c.get(ctx, "/api/config/dhcp/hosts", &body); err != nil {
		return nil, err
	}
	var out []Reservation
	for _, h := range body.Config.DHCP.Hosts {
		parts := strings.Split(h, ",")
		if len(parts) < 2 {
			continue
		}
		mac, ip := normMAC(parts[0]), normIP(parts[1])
		if mac == "" || ip == "" {
			continue
		}
		name := ""
		if len(parts) >= 3 {
			name = strings.TrimSpace(parts[2])
		}
		out = append(out, Reservation{MAC: mac, IP: ip, Hostname: name})
	}
	return out, nil
}

func (c *Client) DNSRecords(ctx context.Context) ([]DNSRecord, error) {
	var body struct {
		Config struct {
			DNS struct {
				Hosts []string `json:"hosts"`
			} `json:"dns"`
		} `json:"config"`
	}
	if err := c.get(ctx, "/api/config/dns/hosts", &body); err != nil {
		return nil, err
	}
	var out []DNSRecord
	for _, h := range body.Config.DNS.Hosts {
		fields := strings.Fields(h)
		if len(fields) < 2 {
			continue
		}
		ip := normIP(fields[0])
		if ip == "" {
			continue
		}
		for _, name := range fields[1:] {
			out = append(out, DNSRecord{IP: ip, Name: name})
		}
	}
	return out, nil
}
