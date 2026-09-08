package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"netis/internal/config"
)

// runHealthcheck probes this host's own /healthz and reports the result as an
// exit status, so a container HEALTHCHECK can use it. It exists as a subcommand
// because the image is distroless: there is no shell, curl or wget inside it,
// and the binary is the only thing that can make the request.
func runHealthcheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	url := "http://" + healthAddr(config.Load().Addr) + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s", url, resp.Status)
	}
	return nil
}

// healthAddr turns a listen address into one that can be dialled. A server
// listening on ":8080" or "0.0.0.0:8080" is reachable at 127.0.0.1:8080 from
// inside the same container; an address already naming a host is used as-is.
func healthAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") { // IPv6 literal
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}
