package proxmox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Guest struct {
	VMID   int64
	Name   string
	Node   string
	Type   string
	Status string
}

type Client struct {
	base   string
	auth   string
	client *http.Client
}

func NewClient(baseURL, tokenID, secret string, insecure bool) *Client {
	tr := &http.Transport{}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		base:   strings.TrimSuffix(baseURL, "/"),
		auth:   fmt.Sprintf("PVEAPIToken=%s=%s", tokenID, secret),
		client: &http.Client{Transport: tr, Timeout: 10 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.auth)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("proxmox %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) ListGuests(ctx context.Context) ([]Guest, error) {
	var body struct {
		Data []struct {
			VMID   int64  `json:"vmid"`
			Name   string `json:"name"`
			Node   string `json:"node"`
			Status string `json:"status"`
			Type   string `json:"type"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/api2/json/cluster/resources?type=vm", &body); err != nil {
		return nil, err
	}
	out := make([]Guest, 0, len(body.Data))
	for _, d := range body.Data {
		out = append(out, Guest{VMID: d.VMID, Name: d.Name, Node: d.Node,
			Type: d.Type, Status: d.Status})
	}
	return out, nil
}

var macRe = regexp.MustCompile(`(?i)\b([0-9a-f]{2}(?::[0-9a-f]{2}){5})\b`)

func (c *Client) GuestMACs(ctx context.Context, node string, vmid int64, typ string) ([]string, error) {
	var body struct {
		Data map[string]any `json:"data"`
	}
	path := fmt.Sprintf("/api2/json/nodes/%s/%s/%d/config", node, typ, vmid)
	if err := c.get(ctx, path, &body); err != nil {
		return nil, err
	}
	var macs []string
	for key, val := range body.Data {
		if !strings.HasPrefix(key, "net") {
			continue
		}
		sval, ok := val.(string)
		if !ok {
			continue
		}
		for _, m := range macRe.FindAllString(sval, -1) {
			macs = append(macs, strings.ToLower(m))
		}
	}
	return macs, nil
}
