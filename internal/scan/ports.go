package scan

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// CommonPorts is the default port list used by an on-demand device port
// scan: common admin/service/media ports worth surfacing on a device page.
var CommonPorts = []int{21, 22, 23, 25, 53, 80, 110, 143, 443, 445, 554, 587,
	631, 993, 995, 1883, 3000, 3306, 3389, 5000, 5432, 5900, 6443, 8000, 8006,
	8080, 8081, 8123, 8443, 9000, 9090, 9100, 32400}

var serviceNames = map[int]string{
	21: "ftp", 22: "ssh", 23: "telnet", 25: "smtp", 53: "dns", 80: "http",
	110: "pop3", 143: "imap", 443: "https", 445: "smb", 554: "rtsp",
	587: "smtp-sub", 631: "ipp", 993: "imaps", 995: "pop3s", 1883: "mqtt",
	3000: "http-alt", 3306: "mysql", 3389: "rdp", 5000: "http-alt",
	5432: "postgres", 5900: "vnc", 6443: "kube-api", 8000: "http-alt",
	8006: "proxmox", 8080: "http-alt", 8081: "http-alt", 8123: "home-assistant",
	8443: "https-alt", 9000: "http-alt", 9090: "prometheus", 9100: "jetdirect",
	32400: "plex",
}

// ServiceGuess returns a best-effort service name for a well-known port,
// or "" if the port isn't in the known set.
func ServiceGuess(port int) string { return serviceNames[port] }

// PortScan attempts a TCP connect to each of ports on ip, using up to 32
// concurrent workers and timeout per connection attempt, and returns the
// sorted list of ports that accepted a connection.
func PortScan(ctx context.Context, ip string, ports []int, timeout time.Duration) []int {
	var (
		mu   sync.Mutex
		open []int
		wg   sync.WaitGroup
	)
	sem := make(chan struct{}, 32)
	d := net.Dialer{Timeout: timeout}
	for _, port := range ports {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", ip, port))
			if err != nil {
				return
			}
			conn.Close()
			mu.Lock()
			open = append(open, port)
			mu.Unlock()
		}(port)
	}
	wg.Wait()
	sort.Ints(open)
	return open
}
