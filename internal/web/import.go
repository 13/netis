package web

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"netis/internal/store"
	"netis/internal/web/views"
)

// CSV import: devices matched by MAC, created when new and enriched when
// known, with a dry run first. The rules for an existing device are the
// project's "enrich, don't clobber" (see store.FillDevice): an import fills in
// what is missing and never overwrites what a user or an integration set.

// maxImportRows bounds one import. The preview renders a row per line.
const maxImportRows = 10000

// importRow is one data line of an import file as read.
type importRow struct {
	line       int
	mac        string // normalised; blank when missing or invalid
	rawMAC     string
	extraMACs  bool
	name, kind string
	ip         string
	tags       []string
	notes      string
	vendor     string
	model      string
	function   string
}

// importPlan is what the import will do, or did, with one row.
type importPlan struct {
	row      importRow
	action   string // create, update, unchanged, error
	deviceID int64
	name     string // the device's name as it stands (updates) or will be (creates)
	changes  []string
	err      string
	// For creates: the validated device.
	nd newDevice
}

// columnAliases maps accepted header names to the column they fill. The
// export's own header is accepted as is.
var columnAliases = map[string]string{
	"mac": "mac", "macs": "mac", "mac address": "mac",
	"name": "name", "kind": "kind",
	"ip": "ip", "ips": "ip", "ip address": "ip",
	"tags": "tags", "notes": "notes", "vendor": "vendor",
	"model": "model", "function": "function",
}

// uncell reverses csvCell: a leading apostrophe in front of a formula
// character was put there by the export.
func uncell(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 1 && v[0] == '\'' && strings.ContainsRune("=+-@\t\r", rune(v[1])) {
		return v[1:]
	}
	return v
}

// splitList splits a multi-valued cell on the export's separator, or on
// commas, which people type into spreadsheets.
func splitList(v string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ';' || r == ',' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseImport reads an import file. It fails as a whole only when the file is
// not CSV or has no MAC column; problems with single rows are left for the
// plan to report against their line.
func parseImport(data []byte) ([]importRow, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // spreadsheet BOM
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, errors.New("the file is empty")
	}
	if err != nil {
		return nil, errors.New("not a CSV file: " + err.Error())
	}
	col := map[string]int{}
	for i, h := range header {
		if c, ok := columnAliases[strings.ToLower(strings.TrimSpace(h))]; ok {
			if _, dup := col[c]; !dup {
				col[c] = i
			}
		}
	}
	if _, ok := col["mac"]; !ok {
		return nil, errors.New("the header has no mac column; devices are matched by MAC address")
	}
	var rows []importRow
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("not a CSV file: " + err.Error())
		}
		line, _ := cr.FieldPos(0)
		get := func(c string) string {
			if i, ok := col[c]; ok && i < len(rec) {
				return uncell(rec[i])
			}
			return ""
		}
		if strings.TrimSpace(strings.Join(rec, "")) == "" {
			continue // a blank line
		}
		if len(rows) == maxImportRows {
			return nil, errors.New("more than " + strconv.Itoa(maxImportRows) + " rows; split the file")
		}
		row := importRow{
			line: line, name: get("name"), kind: strings.ToLower(get("kind")),
			tags: splitList(get("tags")), notes: get("notes"), vendor: get("vendor"),
			model: get("model"), function: get("function"),
		}
		macs := splitList(get("mac"))
		if len(macs) > 0 {
			row.rawMAC = macs[0]
			row.mac, _ = normMAC(macs[0])
			row.extraMACs = len(macs) > 1
		}
		if ips := splitList(get("ip")); len(ips) > 0 {
			row.ip = ips[0]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// planImport decides what each row does, without writing anything.
func (s *Server) planImport(ctx context.Context, rows []importRow) ([]importPlan, error) {
	seen := map[string]int{}
	plans := make([]importPlan, 0, len(rows))
	for _, row := range rows {
		p := importPlan{row: row}
		switch {
		case row.rawMAC == "":
			p.action, p.err = "error", "no MAC address"
		case row.mac == "":
			p.action, p.err = "error", "invalid MAC address "+strconv.Quote(row.rawMAC)
		case seen[row.mac] != 0:
			p.action, p.err = "error", "MAC also on line "+strconv.Itoa(seen[row.mac])
		case row.kind != "" && !validKinds[row.kind]:
			p.action, p.err = "error", "unknown kind "+strconv.Quote(row.kind)
		}
		if p.action == "error" {
			plans = append(plans, p)
			continue
		}
		seen[row.mac] = row.line
		iface, found, err := s.store.FindIfaceByMAC(ctx, row.mac)
		if err != nil {
			return nil, err
		}
		if found {
			if err := s.planUpdate(ctx, &p, iface.DeviceID); err != nil {
				return nil, err
			}
		} else if err := s.planCreate(ctx, &p); err != nil {
			return nil, err
		}
		if row.extraMACs && p.action != "error" {
			p.changes = append(p.changes, "only the first MAC is used")
		}
		plans = append(plans, p)
	}
	return plans, nil
}

func (s *Server) planCreate(ctx context.Context, p *importPlan) error {
	row := p.row
	kind := row.kind
	if kind == "" {
		kind = "other"
	}
	dev := store.Device{
		Name: row.name, Kind: kind, Notes: row.notes, Source: "manual",
		Vendor: row.vendor, Model: row.model, Function: row.function,
	}
	var subnetID int64
	if row.ip != "" {
		id, err := s.subnetContaining(ctx, row.ip)
		if err != nil {
			return err
		}
		subnetID = id
	}
	nd, msg, err := s.checkNewDevice(ctx, dev, row.mac, row.ip, subnetID)
	if err != nil {
		return err
	}
	switch {
	case msg == chooseSubnetMsg:
		p.action, p.err = "error", "no configured subnet contains "+row.ip
	case msg == "name required":
		p.action, p.err = "error", "new device needs a name"
	case msg != "":
		p.action, p.err = "error", msg
	default:
		p.action, p.nd, p.name = "create", nd, nd.dev.Name
		if nd.ip != "" {
			p.changes = append(p.changes, "IP "+nd.ip)
		}
		if len(row.tags) > 0 {
			p.changes = append(p.changes, "tags "+strings.Join(row.tags, ", "))
		}
	}
	return nil
}

// planUpdate works out which of the row's values the existing device would
// take, by the same rules store.FillDevice applies.
func (s *Server) planUpdate(ctx context.Context, p *importPlan, deviceID int64) error {
	d, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	p.deviceID, p.name = d.ID, d.Name
	row := p.row
	if store.ImportMayRename(d) {
		if row.name != "" && row.name != d.Name {
			p.changes = append(p.changes, "name → "+row.name)
		}
		if row.kind != "" && row.kind != d.Kind {
			p.changes = append(p.changes, "kind → "+row.kind)
		}
	}
	for _, f := range []struct{ label, have, offer string }{
		{"notes", d.Notes, row.notes}, {"vendor", d.Vendor, row.vendor},
		{"model", d.Model, row.model}, {"function", d.Function, row.function},
	} {
		if f.have == "" && f.offer != "" {
			p.changes = append(p.changes, f.label+" → "+f.offer)
		}
	}
	if len(row.tags) > 0 {
		have, err := s.store.DeviceTags(ctx, d.ID)
		if err != nil {
			return err
		}
		got := map[string]bool{}
		for _, t := range have {
			got[t.Name] = true
		}
		var add []string
		for _, t := range row.tags {
			if !got[t] {
				add = append(add, t)
				got[t] = true
			}
		}
		if len(add) > 0 {
			p.changes = append(p.changes, "add tags "+strings.Join(add, ", "))
		}
	}
	p.action = "update"
	if len(p.changes) == 0 {
		p.action = "unchanged"
	}
	return nil
}

// applyImport carries out the plan. Error rows are skipped; a row that fails
// on write (a MAC another request took since the preview) becomes an error
// row and the rest carry on.
func (s *Server) applyImport(ctx context.Context, plans []importPlan) error {
	for i := range plans {
		p := &plans[i]
		switch p.action {
		case "create":
			id, err := s.store.CreateDeviceWithIface(ctx, p.nd.dev, p.nd.mac, p.nd.subnetID, p.nd.ip, "static")
			if err != nil {
				if _, msg, ok := writeFailure(err, macTakenMsg, ""); ok {
					p.action, p.err = "error", msg
					continue
				}
				return err
			}
			p.deviceID = id
			if err := s.store.SetDeviceTags(ctx, id, p.row.tags); err != nil {
				return err
			}
		case "update":
			row := p.row
			if err := s.store.FillDevice(ctx, p.deviceID, store.DeviceFill{
				Name: row.name, Kind: row.kind, Notes: row.notes,
				Vendor: row.vendor, Model: row.model, Function: row.function,
			}); err != nil {
				return err
			}
			if err := s.store.AddDeviceTags(ctx, p.deviceID, row.tags); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) handleImportPage(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	s.render(w, r, views.ImportPage(u.Username, views.ImportView{}))
}

// handleImport previews an import, or with commit=1 carries it out. The file
// comes as a multipart upload the first time; the preview page posts the same
// text back in a hidden field, so nothing is kept on the server between the
// two steps.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	u, _ := userFrom(r)
	v := views.ImportView{}
	answer := func(status int) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		s.render(w, r, views.ImportPage(u.Username, v))
	}
	data := []byte(r.FormValue("csv"))
	if f, _, err := r.FormFile("file"); err == nil {
		b, rerr := io.ReadAll(f)
		f.Close()
		if rerr != nil {
			s.fail(w, r, rerr)
			return
		}
		if len(bytes.TrimSpace(b)) > 0 {
			data = b
		}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		v.Error = "choose a CSV file to import"
		answer(http.StatusBadRequest)
		return
	}
	rows, err := parseImport(data)
	if err != nil {
		v.Error = err.Error()
		answer(http.StatusBadRequest)
		return
	}
	plans, err := s.planImport(r.Context(), rows)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if r.FormValue("commit") == "1" {
		if err := s.applyImport(r.Context(), plans); err != nil {
			s.fail(w, r, err)
			return
		}
		v.Committed = true
	} else {
		v.CSV = string(data)
	}
	for _, p := range plans {
		name := p.name
		if name == "" {
			name = p.row.name
		}
		mac := p.row.mac
		if mac == "" {
			mac = p.row.rawMAC
		}
		v.Rows = append(v.Rows, views.ImportRow{
			Line: p.row.line, MAC: mac, Name: name, Action: p.action,
			DeviceID: p.deviceID, Detail: importDetail(p),
		})
		switch p.action {
		case "create":
			v.Creates++
		case "update":
			v.Updates++
		case "unchanged":
			v.Unchanged++
		default:
			v.Errors++
		}
	}
	answer(http.StatusOK)
}

func importDetail(p importPlan) string {
	if p.err != "" {
		return p.err
	}
	return strings.Join(p.changes, "; ")
}
