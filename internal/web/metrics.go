package web

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"netis/internal/buildinfo"
)

// handleMetrics serves the Prometheus text format. netis pulls no metrics
// library in for this: the numbers are four store reads and the exposition
// format is a handful of lines, so the dependency would cost more than it saves.
//
// Access: a bearer token when NETIS_METRICS_TOKEN is set, otherwise a normal
// session. A scraper cannot hold a session cookie, and leaving the endpoint
// open would publish the inventory — every device name, IP and MAC count — to
// anyone who can reach the port.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.metricsAuthorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rows, err := s.store.ListDevices(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	subnets, err := s.store.ListSubnets(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	statuses, err := s.store.ListIntegrationStatus(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}

	online := 0
	for _, row := range rows {
		if row.Online {
			online++
		}
	}

	var b strings.Builder
	info := buildinfo.Get()
	writeMetric(&b, "netis_build_info",
		"Build identity of the running binary; always 1.", "gauge",
		fmt.Sprintf(`{version=%q,commit=%q,build=%q}`, info.Version, info.Commit, info.Build), 1)
	writeMetric(&b, "netis_start_time_seconds",
		"Unix time the process started.", "gauge", "", float64(buildinfo.StartTime().Unix()))
	writeMetric(&b, "netis_devices",
		"Devices in the inventory.", "gauge", "", float64(len(rows)))
	writeMetric(&b, "netis_devices_online",
		"Devices currently seen as online.", "gauge", "", float64(online))
	writeMetric(&b, "netis_subnets",
		"Configured subnets.", "gauge", "", float64(len(subnets)))

	b.WriteString("# HELP netis_subnet_scan_enabled Whether periodic scanning is enabled for a subnet.\n")
	b.WriteString("# TYPE netis_subnet_scan_enabled gauge\n")
	for _, sn := range subnets {
		enabled := 0.0
		if sn.ScanEnabled {
			enabled = 1
		}
		fmt.Fprintf(&b, "netis_subnet_scan_enabled{cidr=%q,name=%q,kind=%q} %s\n",
			sn.CIDR, sn.Name, sn.Kind, formatFloat(enabled))
	}

	b.WriteString("# HELP netis_integration_last_run_seconds Unix time an integration or the scanner last ran.\n")
	b.WriteString("# TYPE netis_integration_last_run_seconds gauge\n")
	for _, it := range statuses {
		ts, err := time.Parse(time.RFC3339, it.LastRun)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "netis_integration_last_run_seconds{name=%q} %s\n",
			it.Name, formatFloat(float64(ts.Unix())))
	}
	b.WriteString("# HELP netis_integration_last_ok Whether an integration's last run succeeded.\n")
	b.WriteString("# TYPE netis_integration_last_ok gauge\n")
	for _, it := range statuses {
		ok := 0.0
		if it.OK {
			ok = 1
		}
		fmt.Fprintf(&b, "netis_integration_last_ok{name=%q} %s\n", it.Name, formatFloat(ok))
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write([]byte(b.String()))
}

// metricsAuthorized reports whether this request may read the metrics: the
// configured scrape token, or a session that requireAuth already validated.
func (s *Server) metricsAuthorized(r *http.Request) bool {
	if s.metricsToken != "" {
		if got := tokenFromRequest(r); got != "" &&
			subtle.ConstantTimeCompare([]byte(got), []byte(s.metricsToken)) == 1 {
			return true
		}
	}
	_, ok := userFrom(r)
	return ok
}

// tokenFromRequest reads a scrape token from an Authorization: Bearer header.
// Prometheus sends it there (authorization.credentials in a scrape config);
// a query parameter is not accepted because it would end up in access logs.
func tokenFromRequest(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return h[len(prefix):]
	}
	return ""
}

// writeMetric writes one metric with its HELP and TYPE lines. labels, when
// given, is the full brace-wrapped label set.
func writeMetric(b *strings.Builder, name, help, typ, labels string, value float64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n%s%s %s\n",
		name, help, name, typ, name, labels, formatFloat(value))
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
