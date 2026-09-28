package config

import (
	"strings"
	"testing"
)

func clearOIDCEnv(t *testing.T) {
	for _, k := range []string{"NETIS_OIDC_ISSUER", "NETIS_OIDC_CLIENT_ID", "NETIS_OIDC_CLIENT_SECRET",
		"NETIS_OIDC_REDIRECT_URL", "NETIS_BASE_URL", "NETIS_OIDC_ADMIN_GROUP", "NETIS_OIDC_GROUPS_CLAIM",
		"NETIS_OIDC_AUTO_CREATE", "NETIS_OIDC_DISABLE_PASSWORD"} {
		t.Setenv(k, "")
	}
}

func TestLoadOIDCOffByDefault(t *testing.T) {
	clearOIDCEnv(t)
	// Stray settings without an issuer do not switch SSO on or fail startup.
	t.Setenv("NETIS_OIDC_CLIENT_ID", "netis")
	o, err := LoadOIDC()
	if err != nil || o.Enabled() {
		t.Fatalf("got %+v, %v; want disabled", o, err)
	}
}

func TestLoadOIDCDefaults(t *testing.T) {
	clearOIDCEnv(t)
	t.Setenv("NETIS_OIDC_ISSUER", "https://auth.example.com")
	t.Setenv("NETIS_OIDC_CLIENT_ID", "netis")
	t.Setenv("NETIS_BASE_URL", "https://netis.example.com/")
	o, err := LoadOIDC()
	if err != nil {
		t.Fatal(err)
	}
	if !o.Enabled() || o.RedirectURL != "https://netis.example.com/auth/oidc/callback" ||
		o.GroupsClaim != "groups" || !o.AutoCreate || o.DisablePassword {
		t.Errorf("got %+v", o)
	}
	// An explicit redirect URL wins over the derived one.
	t.Setenv("NETIS_OIDC_REDIRECT_URL", "https://netis.lan/auth/oidc/callback")
	t.Setenv("NETIS_OIDC_AUTO_CREATE", "false")
	t.Setenv("NETIS_OIDC_DISABLE_PASSWORD", "1")
	o, err = LoadOIDC()
	if err != nil {
		t.Fatal(err)
	}
	if o.RedirectURL != "https://netis.lan/auth/oidc/callback" || o.AutoCreate || !o.DisablePassword {
		t.Errorf("got %+v", o)
	}
}

func TestLoadOIDCRejectsHalfConfigured(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"no client id", map[string]string{"NETIS_BASE_URL": "https://n"}, "CLIENT_ID"},
		{"no redirect", map[string]string{"NETIS_OIDC_CLIENT_ID": "c"}, "REDIRECT_URL"},
		{"bad bool", map[string]string{"NETIS_OIDC_CLIENT_ID": "c", "NETIS_BASE_URL": "https://n",
			"NETIS_OIDC_AUTO_CREATE": "yes please"}, "AUTO_CREATE"},
		{"bad issuer", map[string]string{"NETIS_OIDC_ISSUER": "auth.example.com",
			"NETIS_OIDC_CLIENT_ID": "c", "NETIS_BASE_URL": "https://n"}, "ISSUER"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearOIDCEnv(t)
			t.Setenv("NETIS_OIDC_ISSUER", "https://auth.example.com")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if _, err := LoadOIDC(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want mention of %s", err, tc.want)
			}
		})
	}
}
