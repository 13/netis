package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"netis/internal/store"
	"netis/internal/web/views"
)

const (
	tagColorMsg = "choose a colour from the list"
	tagNameMsg  = "a tag name is 1 to 64 characters"
	tagRaceMsg  = "a tag with that name was just created; try again"
	// maxTagName is the longest tag name, in characters.
	maxTagName = 64
)

// validTagName trims name and reports whether it is 1 to 64 characters.
func validTagName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	return name, n >= 1 && n <= maxTagName
}

// tagFromPath reads the {id} of a /settings/tags/{id}/... route and loads
// that tag. It answers 404 itself when there is none, and reports false.
func (s *Server) tagFromPath(w http.ResponseWriter, r *http.Request) (store.Tag, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return store.Tag{}, false
	}
	tags, err := s.store.ListTags(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return store.Tag{}, false
	}
	for _, t := range tags {
		if t.ID == id {
			auditNote(r).Target = "tag " + t.Name
			return t, true
		}
	}
	http.NotFound(w, r)
	return store.Tag{}, false
}

// tagWriteError answers a failed tag write: a tag deleted meanwhile is a
// 404, a name taken meanwhile a 409 on the Tags page, anything else a
// failure.
func (s *Server) tagWriteError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	s.settingsWriteError(w, r, "tags", err, tagRaceMsg)
}

// tagsDone sends a plain form post back to the Tags page with msg as a toast.
func (s *Server) tagsDone(w http.ResponseWriter, r *http.Request, msg string) {
	s.flashToast(w, r, msg)
	http.Redirect(w, r, "/settings/tags", http.StatusSeeOther)
}

// handleTagColor sets a tag's colour: a palette key, or "" for auto. The
// picker posts it through htmx on change, and the answer redraws the chip and
// toasts; the no-JS Save button gets a redirect.
func (s *Server) handleTagColor(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tagFromPath(w, r)
	if !ok {
		return
	}
	color := r.FormValue("color")
	note := auditNote(r)
	note.Detail = color
	if color == "" {
		note.Detail = "auto"
	}
	if color != "" && !views.IsTagColor(color) {
		note.Detail = tagColorMsg
		if isHTMX(r) {
			http.Error(w, tagColorMsg, http.StatusBadRequest)
			return
		}
		s.settingsError(w, r, "tags", http.StatusBadRequest, tagColorMsg)
		return
	}
	if err := s.store.SetTagColor(r.Context(), t.ID, color); err != nil {
		s.tagWriteError(w, r, err)
		return
	}
	if isHTMX(r) {
		t.Color = color
		s.render(w, r, views.TagColorSaved(t))
		return
	}
	s.tagsDone(w, r, "Colour saved")
}

// handleTagRename renames a tag. A name another tag already has merges the
// two, but only once the form confirms it (confirm=1); until then the Tags
// page asks, in the tag's row, with both device counts.
func (s *Server) handleTagRename(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tagFromPath(w, r)
	if !ok {
		return
	}
	name, ok := validTagName(r.FormValue("name"))
	note := auditNote(r)
	if !ok {
		note.Detail = tagNameMsg
		s.settingsError(w, r, "tags", http.StatusBadRequest, tagNameMsg)
		return
	}
	note.Detail = t.Name + " -> " + name
	merged, err := s.store.RenameTag(r.Context(), t.ID, name, r.FormValue("confirm") == "1")
	if errors.Is(err, store.ErrTagExists) {
		note.Detail += " (merge not confirmed)"
		s.tagMergeConfirm(w, r, t.ID, name)
		return
	}
	if err != nil {
		s.tagWriteError(w, r, err)
		return
	}
	switch {
	case merged != 0:
		note.Detail += " (merged)"
		s.tagsDone(w, r, "Merged into "+name)
	case name == t.Name:
		s.tagsDone(w, r, "Nothing to change")
	default:
		s.tagsDone(w, r, "Renamed to "+name)
	}
}

// tagMergeConfirm answers a rename of tag id to into, a name another tag
// has, with the Tags page asking whether to merge them.
func (s *Server) tagMergeConfirm(w http.ResponseWriter, r *http.Request, id int64, into string) {
	d, err := s.settingsData(r, "tags")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	m := views.TagMerge{ID: id, Into: into}
	for _, tc := range d.Tags {
		switch {
		case tc.ID == id:
			m.Devices = tc.Devices
		case tc.Name == into:
			m.IntoDevices = tc.Devices
		}
	}
	d.TagMerge = m
	u, _ := userFrom(r)
	s.render(w, r, views.SettingsPage(u.Username, d))
}

// handleTagDelete detaches a tag from every device and deletes it.
func (s *Server) handleTagDelete(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tagFromPath(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteTag(r.Context(), t.ID); err != nil {
		s.tagWriteError(w, r, err)
		return
	}
	s.tagsDone(w, r, "Tag deleted")
}
