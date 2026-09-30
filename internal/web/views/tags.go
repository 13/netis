package views

import (
	"hash/fnv"
	"net/url"
	"slices"
	"strings"

	"github.com/a-h/templ"
)

// TagPalette is the tag colours, in the order the picker shows them. Each key
// has a --tag-<key> and --tag-<key>-soft token in app.css.
var TagPalette = []string{"slate", "indigo", "sky", "teal", "violet", "pink", "sand"}

// TagColors maps a tag name to its stored colour ("" or a palette key).
type TagColors map[string]string

// Of returns the stored colour of the named tag, or "" when it has none.
func (c TagColors) Of(name string) string { return c[name] }

// TagColor resolves the palette key a tag is drawn in: its stored key, or,
// for "" and anything that is not a key, the hue its name hashes to.
func TagColor(name, stored string) string {
	if IsTagColor(stored) {
		return stored
	}
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(name)))
	return TagPalette[h.Sum32()%uint32(len(TagPalette))]
}

// IsTagColor reports whether key is a palette key.
func IsTagColor(key string) bool { return slices.Contains(TagPalette, key) }

// tagHref is the device list filtered by the tag.
func tagHref(name string) templ.SafeURL {
	return templ.SafeURL("/devices?tag=" + url.QueryEscape(name))
}
