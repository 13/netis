package views

import "context"

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
