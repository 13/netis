package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"netis/internal/store"
	"netis/internal/web/views"
)

// maxBulkDevices bounds one bulk request, well above any list a person
// selects by hand.
const maxBulkDevices = 2000

// bulkDevices loads the devices a bulk form names (repeated id fields),
// skipping ids that are not numbers or no longer exist, in the order given
// and without repeats.
func (s *Server) bulkDevices(r *http.Request) ([]store.Device, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	var out []store.Device
	for _, raw := range r.PostForm["id"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		if len(seen) > maxBulkDevices {
			break
		}
		d, err := s.store.GetDevice(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// bulkTarget names the devices a bulk action touched for the audit log: the
// first few by name, then how many more.
func bulkTarget(devs []store.Device) string {
	const shown = 5
	var names []string
	for i, d := range devs {
		if i == shown {
			names = append(names, "and "+strconv.Itoa(len(devs)-shown)+" more")
			break
		}
		names = append(names, d.Name)
	}
	return "devices " + strings.Join(names, ", ")
}

// backToList sends the browser back to the devices list with the filter and
// sort the bulk form carried.
func backToList(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, views.ReturnURL(r.PostFormValue("return")), http.StatusSeeOther)
}

// handleDeviceBulkApprove marks the chosen devices reviewed.
func (s *Server) handleDeviceBulkApprove(w http.ResponseWriter, r *http.Request) {
	devs, err := s.bulkDevices(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	auditNote(r).Target = bulkTarget(devs)
	for _, d := range devs {
		if err := s.store.SetDeviceReviewed(r.Context(), d.ID, true); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	backToList(w, r)
}

// handleDeviceBulkTag adds a tag (or several, comma-separated) to the chosen
// devices, keeping the tags they already have.
func (s *Server) handleDeviceBulkTag(w http.ResponseWriter, r *http.Request) {
	devs, err := s.bulkDevices(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tags := parseTags(r.PostFormValue("tag"))
	if len(tags) == 0 {
		http.Error(w, "enter the tag to add", http.StatusBadRequest)
		return
	}
	note := auditNote(r)
	note.Target = bulkTarget(devs)
	note.Detail = "tag " + strings.Join(tags, ", ")
	for _, d := range devs {
		if err := s.store.AddDeviceTags(r.Context(), d.ID, tags); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	backToList(w, r)
}

// handleDeviceBulkDelete deletes the chosen devices once confirmed. Without
// confirm=1 (the list asks in a browser dialog when JavaScript runs) it
// answers with a page that asks first.
func (s *Server) handleDeviceBulkDelete(w http.ResponseWriter, r *http.Request) {
	devs, err := s.bulkDevices(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(devs) == 0 {
		backToList(w, r)
		return
	}
	if r.PostFormValue("confirm") != "1" {
		u, _ := userFrom(r)
		s.render(w, r, views.DeviceBulkDeleteConfirm(u.Username, devs, r.PostFormValue("return")))
		return
	}
	auditNote(r).Target = bulkTarget(devs)
	for _, d := range devs {
		if err := s.store.DeleteDevice(r.Context(), d.ID); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	backToList(w, r)
}
