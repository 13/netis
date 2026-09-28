// Package adguard reads DHCP leases from AdGuard Home's built-in DHCP server.
package adguard

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"netis/internal/leases"
)

// DHCP is what AdGuard Home reports about its DHCP server: whether it is on,
// its current leases and its static leases (reservations). Entries without a
// valid MAC or IP are dropped.
type DHCP struct {
	Enabled bool
	Leases  []leases.Entry
	Static  []leases.Entry
}

// Client talks to the AdGuard Home control API with HTTP basic auth.
type Client struct {
	base     string
	user     string
	password string
	http     *http.Client
}

func NewClient(baseURL, user, password string, insecure bool) *Client {
	tr := &http.Transport{}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		base:     strings.TrimSuffix(baseURL, "/"),
		user:     user,
		password: password,
		http:     &http.Client{Transport: tr, Timeout: 10 * time.Second},
	}
}

// Close releases the client's idle connections.
func (c *Client) Close() { c.http.CloseIdleConnections() }

type lease struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
}

// DHCPStatus fetches GET /control/dhcp/status.
func (c *Client) DHCPStatus(ctx context.Context) (DHCP, error) {
	const path = "/control/dhcp/status"
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	if err != nil {
		return DHCP{}, err
	}
	req.SetBasicAuth(c.user, c.password)
	resp, err := c.http.Do(req)
	if err != nil {
		return DHCP{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		io.Copy(io.Discard, resp.Body) // drain so the connection can be reused
		return DHCP{}, fmt.Errorf("adguard %s: HTTP %d", path, resp.StatusCode)
	}
	var body struct {
		Enabled      bool    `json:"enabled"`
		Leases       []lease `json:"leases"`
		StaticLeases []lease `json:"static_leases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return DHCP{}, fmt.Errorf("adguard %s: %w", path, err)
	}
	return DHCP{Enabled: body.Enabled, Leases: entries(body.Leases), Static: entries(body.StaticLeases)}, nil
}

func entries(in []lease) []leases.Entry {
	out := make([]leases.Entry, 0, len(in))
	for _, l := range in {
		ip, mac := leases.NormIP(l.IP), leases.NormMAC(l.MAC)
		if ip == "" || mac == "" {
			continue
		}
		out = append(out, leases.Entry{MAC: mac, IP: ip, Hostname: strings.TrimSpace(l.Hostname)})
	}
	return out
}
