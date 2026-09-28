package web

import (
	"encoding/csv"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Inventory export, for backups and spreadsheets. Readable by every role, like
// the rest of the read API, with a session or an API token.

type exportDevice struct {
	ID       int64         `json:"id"`
	Name     string        `json:"name"`
	Kind     string        `json:"kind"`
	Source   string        `json:"source"`
	Vendor   string        `json:"vendor"`
	Model    string        `json:"model"`
	Function string        `json:"function"`
	Notes    string        `json:"notes"`
	Tags     []string      `json:"tags"`
	Online   bool          `json:"online"`
	LastSeen *string       `json:"last_seen"`
	Ifaces   []exportIface `json:"ifaces"`
}

type exportIface struct {
	MAC      *string    `json:"mac"`
	Hostname *string    `json:"hostname,omitempty"`
	IPs      []exportIP `json:"ips"`
}

type exportIP struct {
	IP     string `json:"ip"`
	Kind   string `json:"kind"`
	Subnet string `json:"subnet"`
}

// exportDevices assembles the export: the device list plus each device's
// interfaces with the addresses they hold. Two store reads for the lot.
func (s *Server) exportDevices(r *http.Request) ([]exportDevice, error) {
	rows, err := s.store.ListDevices(r.Context())
	if err != nil {
		return nil, err
	}
	ifaces, err := s.store.ListExportIfaces(r.Context())
	if err != nil {
		return nil, err
	}
	out := make([]exportDevice, 0, len(rows))
	for _, row := range rows {
		d := exportDevice{
			ID: row.ID, Name: row.Name, Kind: row.Kind, Source: row.Source,
			Vendor: row.Vendor, Model: row.Model, Function: row.Function, Notes: row.Notes,
			Tags: row.TagNames, Online: row.Online, LastSeen: row.LastSeen,
			Ifaces: []exportIface{},
		}
		if d.Tags == nil {
			d.Tags = []string{}
		}
		for _, f := range ifaces[row.ID] {
			ef := exportIface{MAC: f.MAC, Hostname: f.Hostname, IPs: []exportIP{}}
			for _, ip := range f.IPs {
				ef.IPs = append(ef.IPs, exportIP{IP: ip.IP, Kind: ip.Kind, Subnet: ip.Subnet})
			}
			d.Ifaces = append(d.Ifaces, ef)
		}
		out = append(out, d)
	}
	return out, nil
}

// exportFilename is the download name, dated so successive exports do not
// overwrite each other in a downloads folder.
func exportFilename(ext string) string {
	return "netis-devices-" + time.Now().UTC().Format("2006-01-02") + "." + ext
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	devs, err := s.exportDevices(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+exportFilename("json")+`"`)
	s.writeJSON(w, r, map[string]any{"devices": devs})
}

// csvHeader is the export's column list. The import reads the same names, so
// an export can be edited and fed back in.
var csvHeader = []string{"id", "name", "kind", "source", "mac", "ips", "tags",
	"notes", "vendor", "model", "function", "online", "last_seen"}

// csvListSep joins the multi-valued columns (MACs, IPs, tags). Not a comma, so
// a cell holding a list does not need quoting to be read by eye.
const csvListSep = ";"

// csvCell makes a value safe to open in a spreadsheet. A cell beginning with
// one of these characters is evaluated as a formula by Excel, LibreOffice and
// Sheets, and device names, notes and hostnames come from the network and from
// other users; a leading apostrophe makes the cell literal text. The import
// strips it again.
func csvCell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	devs, err := s.exportDevices(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+exportFilename("csv")+`"`)
	cw := csv.NewWriter(w)
	cw.Write(csvHeader)
	for _, d := range devs {
		var macs, ips []string
		for _, f := range d.Ifaces {
			if f.MAC != nil {
				macs = append(macs, *f.MAC)
			}
			for _, ip := range f.IPs {
				ips = append(ips, ip.IP)
			}
		}
		lastSeen := ""
		if d.LastSeen != nil {
			lastSeen = *d.LastSeen
		}
		rec := []string{strconv.FormatInt(d.ID, 10), d.Name, d.Kind, d.Source,
			strings.Join(macs, csvListSep), strings.Join(ips, csvListSep), strings.Join(d.Tags, csvListSep),
			d.Notes, d.Vendor, d.Model, d.Function, strconv.FormatBool(d.Online), lastSeen}
		for i := range rec {
			rec[i] = csvCell(rec[i])
		}
		cw.Write(rec)
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		// The status line is out; all that is left is to say so in the log.
		slog.Warn("writing csv export failed", "err", err)
	}
}
