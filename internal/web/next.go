package web

import "strings"

// localNext is where a form sends the user back to: next when it is a path
// on this site, fallback otherwise. A scheme, host or protocol-relative URL
// ("//evil.example", "/\evil.example") is refused so the form cannot be used
// as an open redirect.
func localNext(next, fallback string) string {
	if next == "" || next[0] != '/' || strings.HasPrefix(next, "//") || strings.HasPrefix(next, `/\`) ||
		strings.ContainsAny(next, "\r\n") {
		return fallback
	}
	return next
}
