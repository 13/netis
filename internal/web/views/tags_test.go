package views

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestTagColor(t *testing.T) {
	if got := TagColor("nas", "teal"); got != "teal" {
		t.Errorf("stored key: TagColor = %q, want teal", got)
	}
	auto := TagColor("nas", "")
	if !slices.Contains(TagPalette, auto) {
		t.Fatalf("auto colour %q is not a palette key", auto)
	}
	if got := TagColor("NAS", ""); got != auto {
		t.Errorf("auto colour depends on case: %q vs %q", got, auto)
	}
	for _, stored := range []string{"#888888", "Teal", "green", "<b>"} {
		if got := TagColor("nas", stored); got != auto {
			t.Errorf("TagColor(nas, %q) = %q, want the auto colour %q", stored, got, auto)
		}
	}
}

// The auto hue is FNV-1a 32-bit over the lower-cased name, mod 7, the same
// algorithm static/tags.js runs client-side (autoColor there) and
// e2e/tests/tags.spec.ts checks against. These pairs are pinned literals, not
// recomputed from TagColor, so a change to the hash or to TagPalette's key
// order fails here instead of silently drifting from the JS copy.
func TestTagColorPinned(t *testing.T) {
	for name, want := range map[string]string{
		"garage": "indigo",
		"cam":    "sky",
		"office": "indigo",
	} {
		if got := TagColor(name, ""); got != want {
			t.Errorf("TagColor(%q, \"\") = %q, want %q", name, got, want)
		}
	}
}

// The hash spreads names over the whole palette.
func TestTagColorUsesEveryHue(t *testing.T) {
	seen := map[string]bool{}
	for i := range 100 {
		seen[TagColor(fmt.Sprintf("tag%d", i), "")] = true
	}
	for _, k := range TagPalette {
		if !seen[k] {
			t.Errorf("no name among tag0..tag99 hashes to %q", k)
		}
	}
}

func TestIsTagColor(t *testing.T) {
	for _, k := range TagPalette {
		if !IsTagColor(k) {
			t.Errorf("IsTagColor(%q) = false", k)
		}
	}
	for _, k := range []string{"", "#888888", "green", "Teal"} {
		if IsTagColor(k) {
			t.Errorf("IsTagColor(%q) = true", k)
		}
	}
}

func TestTagColorsOf(t *testing.T) {
	c := TagColors{"nas": "teal"}
	if c.Of("nas") != "teal" || c.Of("other") != "" || TagColors(nil).Of("x") != "" {
		t.Errorf("TagColors.Of wrong: %q %q", c.Of("nas"), c.Of("other"))
	}
}

func TestTagChip(t *testing.T) {
	var b strings.Builder
	if err := TagChip("media & more", "sky").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{
		`class="tag tag-sky"`,
		`href="/devices?tag=media+%26+more"`,
		`title="media &amp; more"`,
		`>media &amp; more</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("TagChip = %s\nmissing %s", got, want)
		}
	}
}

func TestTagSpan(t *testing.T) {
	var b strings.Builder
	if err := TagSpan("<x>", "").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	want := `<span class="tag tag-` + TagColor("<x>", "") + `" title="&lt;x&gt;">&lt;x&gt;</span>`
	if got != want {
		t.Errorf("TagSpan = %s, want %s", got, want)
	}
}
