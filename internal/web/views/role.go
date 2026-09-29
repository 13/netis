package views

import (
	"context"
	"strings"
)

type adminKey struct{}

// WithAdmin records on ctx whether the signed-in user is an admin, for the
// templates to read with IsAdmin. The server sets it on every render, so the
// flag does not have to be threaded through each page's arguments.
func WithAdmin(ctx context.Context, admin bool) context.Context {
	return context.WithValue(ctx, adminKey{}, admin)
}

// IsAdmin reports whether the page is being rendered for an admin. Anything
// rendered without the flag is treated as a viewer's page: forms and buttons
// that only an admin can use are left out rather than shown to someone who
// would get 403 for clicking them.
func IsAdmin(ctx context.Context) bool {
	admin, _ := ctx.Value(adminKey{}).(bool)
	return admin
}

type pathKey struct{}

// WithPath records the request path on ctx, so the navigation can mark the
// page it is on without every page passing it along.
func WithPath(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, pathKey{}, path)
}

// currentPath is the path WithPath recorded, or "" when rendered without one.
func currentPath(ctx context.Context) string {
	p, _ := ctx.Value(pathKey{}).(string)
	return p
}

// navActive reports whether a navigation link to href belongs to the page at
// path: the dashboard only on "/", every other section on its own path and
// anything under it (a device's page is under Devices).
func navActive(path, href string) bool {
	if href == "/" {
		return path == "/"
	}
	return path == href || strings.HasPrefix(path, href+"/")
}

// roleLabel names a role for people: "Admin" or "Viewer".
func roleLabel(role string) string {
	switch role {
	case "admin":
		return "Admin"
	case "viewer":
		return "Viewer"
	}
	return role
}

// Sentence gives a message its sentence case: the first letter upper case.
// Handler messages are written lower case, as Go errors are, and the API
// sends them that way; a page shows them as the sentence they are.
func Sentence(msg string) string {
	for i, r := range msg {
		return strings.ToUpper(string(r)) + msg[i+len(string(r)):]
	}
	return msg
}
