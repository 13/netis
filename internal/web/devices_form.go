package web

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"netis/internal/autofill"
	"netis/internal/store"
	"netis/internal/web/views"
)

// deviceFormLists loads the subnets, devices and tags the device form offers
// as choices.
func (s *Server) deviceFormLists(r *http.Request, f *views.DeviceForm) error {
	var err error
	if f.Subnets, err = s.store.ListSubnets(r.Context()); err != nil {
		return err
	}
	if f.AllDevices, err = s.store.ListDevices(r.Context()); err != nil {
		return err
	}
	if f.AllTags, err = s.store.ListTags(r.Context()); err != nil {
		return err
	}
	return s.deviceFormSuggest(r.Context(), f)
}

// suggestFields are the text fields the device form offers known values for.
var suggestFields = []string{autofill.FieldVendor, autofill.FieldModel, autofill.FieldFunction}

// deviceFormSuggest fills the form's suggestions: the values other devices
// hold, and on the edit form the device's own hints, plus what autofill
// detected where it differs from what the device holds.
func (s *Server) deviceFormSuggest(ctx context.Context, f *views.DeviceForm) error {
	f.Suggest = map[string][]string{}
	for _, field := range suggestFields {
		vals, err := s.store.DistinctDeviceValues(ctx, field)
		if err != nil {
			return err
		}
		f.Suggest[field] = vals
	}
	if !f.IsEdit || f.Device.ID == 0 {
		return nil
	}
	hints, err := s.store.ListHints(ctx, f.Device.ID)
	if err != nil {
		return err
	}
	for _, field := range suggestFields {
		vals := f.Suggest[field]
		for _, h := range hints {
			if h.Field == field && h.Value != "" && !slices.Contains(vals, h.Value) {
				vals = append(vals, h.Value)
			}
		}
		slices.Sort(vals)
		f.Suggest[field] = vals
	}
	fields, tags := autofill.Resolve(hints)
	current := map[string]string{
		autofill.FieldVendor: f.Device.Vendor, autofill.FieldModel: f.Device.Model,
		autofill.FieldFunction: f.Device.Function, autofill.FieldKind: f.Device.Kind,
	}
	f.Detected = map[string]string{}
	for field, cur := range current {
		if c, ok := fields[field]; ok && c.Value != cur {
			f.Detected[field] = c.Value
		}
	}
	for _, c := range tags {
		if !slices.ContainsFunc(f.Tags, func(t store.Tag) bool { return strings.EqualFold(t.Name, c.Value) }) {
			f.DetectedTags = append(f.DetectedTags, c.Value)
		}
	}
	return nil
}

// renderDeviceForm answers with the device form: the dialog alone for htmx, which swaps it into #modal, and a page of its own
// otherwise, so the New and Edit links work without JavaScript and a plain
// post that failed shows its error with the form. status is the code to
// send, 200 for a fresh form.
func (s *Server) renderDeviceForm(w http.ResponseWriter, r *http.Request, f views.DeviceForm, status int) {
	if err := s.deviceFormLists(r, &f); err != nil {
		s.fail(w, r, err)
		return
	}
	if f.SubnetID == 0 {
		f.SubnetID = subnetFor(f.Subnets, f.IP)
	}
	var c templ.Component
	switch {
	case !isHTMX(r):
		u, _ := userFrom(r)
		c = views.DeviceFormPage(u.Username, f)
	default:
		c = views.DeviceDialog(f)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.render(w, r, c)
}

// submittedDeviceForm is the form as the user filled it in, to send back
// with an error.
func submittedDeviceForm(r *http.Request, d store.Device, isEdit bool) views.DeviceForm {
	f := views.DeviceForm{Device: d, IsEdit: isEdit,
		MAC: r.FormValue("mac"), IP: r.FormValue("ip")}
	f.SubnetID, _ = strconv.ParseInt(r.FormValue("subnet_id"), 10, 64)
	for _, name := range parseTags(r.FormValue("tags")) {
		f.Tags = append(f.Tags, store.Tag{Name: name})
	}
	return f
}

// redirectAfterForm sends the browser on to url once a form has saved. A
// dialog posts through htmx, which would follow a plain redirect itself and
// swap the next page into the dialog, so it is told to navigate instead.
func redirectAfterForm(w http.ResponseWriter, r *http.Request, url string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func (s *Server) handleDeviceForm(w http.ResponseWriter, r *http.Request) {
	f := views.DeviceForm{Device: store.Device{Kind: "computer"}, IP: r.URL.Query().Get("ip")}
	if v := r.URL.Query().Get("subnet"); v != "" {
		f.SubnetID, _ = strconv.ParseInt(v, 10, 64)
	}
	f.Prefilled = f.IP != ""
	s.renderDeviceForm(w, r, f, http.StatusOK)
}

func (s *Server) handleDeviceEditForm(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	tags, err := s.store.DeviceTags(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderDeviceForm(w, r, views.DeviceForm{Device: d, Tags: tags, IsEdit: true}, http.StatusOK)
}

func (s *Server) handleDeviceCreate(w http.ResponseWriter, r *http.Request) {
	dev := store.Device{
		Name: r.FormValue("name"), Kind: r.FormValue("kind"), Notes: r.FormValue("notes"),
		Icon: r.FormValue("icon"), Source: "manual",
		Vendor: r.FormValue("vendor"), Model: r.FormValue("model"), Function: r.FormValue("function"),
	}
	if p := r.FormValue("parent_device_id"); p != "" {
		if pid, err := strconv.ParseInt(p, 10, 64); err == nil {
			dev.ParentDeviceID = &pid
		}
	}
	// A refused form comes back filled in as submitted, with the reason.
	refuse := func(status int, msg string) {
		f := submittedDeviceForm(r, dev, false)
		f.Error = msg
		s.renderDeviceForm(w, r, f, status)
	}
	auditNote(r).Target = "device " + dev.Name
	if !store.TagNamesFit(parseTags(r.FormValue("tags"))) {
		refuse(http.StatusBadRequest, store.TagNameMsg)
		return
	}
	// A subnet_id that is missing or not a number is "not chosen" (zero).
	subnetID, _ := strconv.ParseInt(r.FormValue("subnet_id"), 10, 64)
	nd, msg, err := s.checkNewDevice(r.Context(), dev, r.FormValue("mac"), r.FormValue("ip"), subnetID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if msg != "" {
		refuse(http.StatusBadRequest, msg)
		return
	}
	devID, err := s.store.CreateDeviceWithIface(r.Context(), nd.dev, nd.mac, nd.subnetID, nd.ip, "static")
	if err != nil {
		if status, msg, ok := writeFailure(err, macTakenMsg, parentMissingMsg); ok {
			refuse(status, msg)
			return
		}
		s.fail(w, r, err)
		return
	}
	if err := s.store.SetDeviceTags(r.Context(), devID, parseTags(r.FormValue("tags"))); err != nil {
		s.fail(w, r, err)
		return
	}
	s.kickAutofill()
	s.flashToast(w, r, "Device created")
	redirectAfterForm(w, r, "/devices/"+strconv.FormatInt(devID, 10))
}

const (
	macTakenMsg      = "that MAC address already belongs to another device"
	parentMissingMsg = "parent device does not exist"
	chooseSubnetMsg  = "choose the subnet the IP belongs to"
)

// newDevice is a validated create request, ready for CreateDeviceWithIface.
type newDevice struct {
	dev      store.Device
	mac      *string
	subnetID int64
	ip       string
}

// checkNewDevice validates a device create request, from the HTML form or the
// JSON API, so both refuse the same things with the same words. mac and ip
// are as submitted (blank for none); subnetID is zero when none was chosen.
// msg says what is wrong with the request; err is a failure to look the
// subnet up.
func (s *Server) checkNewDevice(ctx context.Context, dev store.Device, mac, ip string, subnetID int64) (nd newDevice, msg string, err error) {
	dev.Name = strings.TrimSpace(dev.Name)
	if !validKinds[dev.Kind] {
		return nd, "bad kind", nil
	}
	if dev.Name == "" {
		return nd, "name required", nil
	}
	nd.dev = dev
	if raw := strings.TrimSpace(mac); raw != "" {
		m, ok := normMAC(raw)
		if !ok {
			return nd, "invalid MAC address", nil
		}
		nd.mac = &m
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nd, "", nil
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return nd, "invalid IP address", nil
	}
	if subnetID == 0 {
		return nd, chooseSubnetMsg, nil
	}
	sn, err := s.store.GetSubnet(ctx, subnetID)
	if errors.Is(err, sql.ErrNoRows) {
		return nd, "unknown subnet", nil
	}
	if err != nil {
		return nd, "", err
	}
	prefix, err := netip.ParsePrefix(sn.CIDR)
	if err != nil {
		return nd, "", err
	}
	if !prefix.Contains(addr) {
		return nd, "IP " + addr.String() + " is not in subnet " + sn.CIDR, nil
	}
	nd.subnetID, nd.ip = sn.ID, addr.String()
	return nd, "", nil
}

func (s *Server) handleDeviceUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	auditNote(r).Target = deviceTarget(d)
	if kind := r.FormValue("kind"); validKinds[kind] {
		d.Kind = kind
	}
	if name := strings.TrimSpace(r.FormValue("name")); name != "" {
		d.Name = name
	}
	d.Notes = r.FormValue("notes")
	d.Icon = r.FormValue("icon")
	d.Vendor = r.FormValue("vendor")
	d.Model = r.FormValue("model")
	d.Function = r.FormValue("function")
	if p := r.FormValue("parent_device_id"); p != "" {
		if pid, err := strconv.ParseInt(p, 10, 64); err == nil && pid != d.ID {
			d.ParentDeviceID = &pid
		}
	} else {
		d.ParentDeviceID = nil
	}
	if !store.TagNamesFit(parseTags(r.FormValue("tags"))) {
		f := submittedDeviceForm(r, d, true)
		f.Error = store.TagNameMsg
		s.renderDeviceForm(w, r, f, http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateDevice(r.Context(), d); err != nil {
		if status, msg, ok := writeFailure(err, "device conflicts with an existing one", parentMissingMsg); ok {
			f := submittedDeviceForm(r, d, true)
			f.Error = msg
			s.renderDeviceForm(w, r, f, status)
			return
		}
		s.fail(w, r, err)
		return
	}
	if err := s.store.SetDeviceTags(r.Context(), d.ID, parseTags(r.FormValue("tags"))); err != nil {
		s.fail(w, r, err)
		return
	}
	s.flashToast(w, r, "Changes saved")
	redirectAfterForm(w, r, "/devices/"+r.PathValue("id"))
}
