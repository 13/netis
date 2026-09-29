// Package netis holds repository-level tests that belong to no Go package:
// the README and docs/ link check.
package netis

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// Every relative link and image in README.md and docs/*.md points at a file
// that exists, and every #anchor at a heading in its target. External links
// are not fetched.
func TestDocsLinks(t *testing.T) {
	files, err := filepath.Glob("docs/*.md")
	if err != nil {
		t.Fatal(err)
	}
	files = append([]string{"README.md"}, files...)
	anchors := map[string]map[string]bool{}
	headings := func(path string) map[string]bool {
		if a, ok := anchors[path]; ok {
			return a
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		anchors[path] = headingAnchors(string(b))
		return anchors[path]
	}
	checked := 0
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range docLinks(string(b)) {
			target, frag, _ := strings.Cut(l.target, "#")
			path := file
			if target != "" {
				path = filepath.Join(filepath.Dir(file), filepath.FromSlash(target))
				if _, err := os.Stat(path); err != nil {
					t.Errorf("%s:%d: %s: no such file %s", file, l.line, l.target, path)
					continue
				}
			}
			checked++
			if frag == "" {
				continue
			}
			if !strings.HasSuffix(path, ".md") {
				t.Errorf("%s:%d: %s: anchor on a file that is not markdown", file, l.line, l.target)
				continue
			}
			if !headings(path)[frag] {
				t.Errorf("%s:%d: %s: no heading #%s in %s", file, l.line, l.target, frag, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no links at all; the parser is broken")
	}
}

type docLink struct {
	target string
	line   int
}

var (
	mdLink   = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	htmlLink = regexp.MustCompile(`\b(?:src|href|srcset)="([^"]+)"`)
	inline   = regexp.MustCompile("`[^`]*`")
)

// docLinks returns the relative link and image targets in a markdown file,
// skipping fenced code blocks, inline code and external URLs.
func docLinks(md string) []docLink {
	var out []docLink
	fenced := false
	for i, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		line = inline.ReplaceAllString(line, "")
		var targets []string
		for _, m := range mdLink.FindAllStringSubmatch(line, -1) {
			targets = append(targets, m[1])
		}
		for _, m := range htmlLink.FindAllStringSubmatch(line, -1) {
			// srcset may list "url 2x, url 1x"; the first word of each is a URL.
			for _, part := range strings.Split(m[1], ",") {
				if f := strings.Fields(part); len(f) > 0 {
					targets = append(targets, f[0])
				}
			}
		}
		for _, t := range targets {
			if strings.Contains(t, "://") || strings.HasPrefix(t, "mailto:") {
				continue
			}
			out = append(out, docLink{target: t, line: i + 1})
		}
	}
	return out
}

var (
	atxHeading = regexp.MustCompile(`^#{1,6}\s+(.*?)\s*#*\s*$`)
	headLink   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

// headingAnchors returns the anchors GitHub gives a file's headings.
func headingAnchors(md string) map[string]bool {
	out := map[string]bool{}
	seen := map[string]int{}
	fenced := false
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		m := atxHeading.FindStringSubmatch(line)
		if fenced || m == nil {
			continue
		}
		s := slug(m[1])
		if n := seen[s]; n > 0 {
			out[s+"-"+strconv.Itoa(n)] = true
		} else {
			out[s] = true
		}
		seen[s]++
	}
	return out
}

// slug is GitHub's heading anchor: the text of the heading, lower-cased,
// with punctuation other than hyphens and underscores dropped and spaces
// turned into hyphens.
func slug(heading string) string {
	heading = headLink.ReplaceAllString(heading, "$1")
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Single sign-on (OIDC)":         "single-sign-on-oidc",
		"Pi-hole (v6)":                  "pi-hole-v6",
		"AdGuard Home and OPNsense":     "adguard-home-and-opnsense",
		"Using `netis` [here](x.md)":    "using-netis-here",
		"Users, sign-in and security":   "users-sign-in-and-security",
		"JSON API, export and metrics":  "json-api-export-and-metrics",
		"Discovery → Limitations":       "discovery--limitations",
		"Keycloak and Pocket ID":        "keycloak-and-pocket-id",
		"Proxmox LXC with systemd":      "proxmox-lxc-with-systemd",
		"Backups and database backends": "backups-and-database-backends",
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
	a := headingAnchors("# A\n## A\n```\n# B\n```\n### A\n")
	for _, want := range []string{"a", "a-1", "a-2"} {
		if !a[want] {
			t.Errorf("anchor %q missing from %v", want, a)
		}
	}
	if a["b"] {
		t.Error("heading inside a code fence counted")
	}
}
