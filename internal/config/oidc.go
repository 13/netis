package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// OIDC is the OpenID Connect login configuration. It comes from the
// environment rather than the settings table because it is deployment
// configuration with a client secret in it, not something an admin should be
// able to read back or change from the UI.
type OIDC struct {
	// Issuer is NETIS_OIDC_ISSUER. Empty means SSO is off and every other
	// field is meaningless.
	Issuer       string
	ClientID     string
	ClientSecret string // empty for a public client, which relies on PKCE alone
	// RedirectURL is the callback registered at the provider:
	// NETIS_OIDC_REDIRECT_URL, or NETIS_BASE_URL + /auth/oidc/callback.
	RedirectURL string
	// AdminGroup members are made admins and everyone else viewers, on every
	// login. Empty means SSO leaves roles alone.
	AdminGroup  string
	GroupsClaim string
	// AutoCreate creates a netis user on the first login of an unknown
	// identity; off, such a login is refused.
	AutoCreate bool
	// DisablePassword hides the password form and restricts password login to
	// local (unlinked) admins, the break-glass for an unreachable provider.
	DisablePassword bool
}

// Enabled reports whether SSO login is configured.
func (o OIDC) Enabled() bool { return o.Issuer != "" }

// OIDCCallbackPath is where the provider sends the browser back to.
const OIDCCallbackPath = "/auth/oidc/callback"

// LoadOIDC reads the OIDC settings from the environment. A half-configured
// provider is an error the caller is expected to exit on: an SSO button that
// can never work is worse than refusing to start.
func LoadOIDC() (OIDC, error) {
	o := OIDC{
		Issuer:       strings.TrimSpace(os.Getenv("NETIS_OIDC_ISSUER")),
		ClientID:     strings.TrimSpace(os.Getenv("NETIS_OIDC_CLIENT_ID")),
		ClientSecret: os.Getenv("NETIS_OIDC_CLIENT_SECRET"),
		RedirectURL:  strings.TrimSpace(os.Getenv("NETIS_OIDC_REDIRECT_URL")),
		AdminGroup:   strings.TrimSpace(os.Getenv("NETIS_OIDC_ADMIN_GROUP")),
		GroupsClaim:  strings.TrimSpace(os.Getenv("NETIS_OIDC_GROUPS_CLAIM")),
	}
	if o.Issuer == "" {
		return OIDC{}, nil
	}
	if o.GroupsClaim == "" {
		o.GroupsClaim = "groups"
	}
	var err error
	if o.AutoCreate, err = envBool("NETIS_OIDC_AUTO_CREATE", true); err != nil {
		return OIDC{}, err
	}
	if o.DisablePassword, err = envBool("NETIS_OIDC_DISABLE_PASSWORD", false); err != nil {
		return OIDC{}, err
	}
	if u, err := url.Parse(o.Issuer); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return OIDC{}, fmt.Errorf("NETIS_OIDC_ISSUER %q is not an http(s) URL", o.Issuer)
	}
	if o.ClientID == "" {
		return OIDC{}, fmt.Errorf("NETIS_OIDC_CLIENT_ID is required when NETIS_OIDC_ISSUER is set")
	}
	if o.RedirectURL == "" {
		base := strings.TrimRight(strings.TrimSpace(os.Getenv("NETIS_BASE_URL")), "/")
		if base == "" {
			return OIDC{}, fmt.Errorf("NETIS_OIDC_REDIRECT_URL or NETIS_BASE_URL is required when NETIS_OIDC_ISSUER is set")
		}
		o.RedirectURL = base + OIDCCallbackPath
	}
	if u, err := url.Parse(o.RedirectURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return OIDC{}, fmt.Errorf("OIDC redirect URL %q is not an http(s) URL", o.RedirectURL)
	}
	return o, nil
}

// envBool reads a boolean, returning def when it is unset and an error when it
// is set to something strconv.ParseBool does not accept.
func envBool(key string, def bool) (bool, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s=%q is not a boolean (use true or false)", key, v)
	}
	return b, nil
}
