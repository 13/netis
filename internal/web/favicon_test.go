package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func anonGet(srv *Server, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

// The manifest is fetched without credentials and only honoured when served
// as application/manifest+json; every icon it lists must exist.
func TestManifestServedWithItsType(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	rec := anonGet(srv, "/static/manifest.webmanifest")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("Content-Type %q", ct)
	}
	var m struct {
		Name, StartURL, Display string
		Icons                   []struct{ Src, Type, Purpose string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "netis" || len(m.Icons) == 0 {
		t.Fatalf("manifest %+v", m)
	}
	maskable := false
	for _, ic := range m.Icons {
		r := anonGet(srv, ic.Src)
		if r.Code != http.StatusOK || r.Header().Get("Content-Type") != ic.Type {
			t.Errorf("%s: status %d, type %q, want %q", ic.Src, r.Code, r.Header().Get("Content-Type"), ic.Type)
		}
		maskable = maskable || ic.Purpose == "maskable"
	}
	if !maskable {
		t.Error("no maskable icon")
	}
}

// Browsers ask for /favicon.ico on their own, signed in or not; a login
// redirect there would leave the tab without an icon.
func TestFaviconAtRootIsPublic(t *testing.T) {
	srv, st := testServer(t)
	addAdmin(t, st)
	rec := anonGet(srv, "/favicon.ico")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/x-icon" {
		t.Errorf("Content-Type %q", ct)
	}
	want, _ := staticFS.ReadFile("static/favicon.ico")
	if rec.Body.String() != string(want) {
		t.Error("body is not the embedded favicon.ico")
	}
	for _, p := range []string{"/static/favicon.svg", "/static/apple-touch-icon.png"} {
		if r := anonGet(srv, p); r.Code != http.StatusOK {
			t.Errorf("%s: status %d", p, r.Code)
		}
	}
}

// Every page shell links the icons, the manifest and a theme colour per scheme.
func TestPagesLinkIconsAndManifest(t *testing.T) {
	want := []string{
		`<link rel="icon" href="/favicon.ico" sizes="32x32">`,
		`<link rel="icon" href="/static/favicon.svg" type="image/svg+xml">`,
		`<link rel="apple-touch-icon" href="/static/apple-touch-icon.png">`,
		`<link rel="manifest" href="/static/manifest.webmanifest">`,
		`<meta name="theme-color" content="#ECEEEC" media="(prefers-color-scheme: light)">`,
		`<meta name="theme-color" content="#161A18" media="(prefers-color-scheme: dark)">`,
	}
	check := func(page, body string) {
		t.Helper()
		head, _, _ := strings.Cut(body, "</head>")
		for _, w := range want {
			if !strings.Contains(head, w) {
				t.Errorf("%s: head lacks %s", page, w)
			}
		}
	}

	srv, _ := testServer(t)
	check("/setup", anonGet(srv, "/setup").Body.String())

	srv, st := testServer(t)
	addAdmin(t, st)
	check("/login", anonGet(srv, "/login").Body.String())
	check("/welcome", authedGet(t, srv, st, "/welcome").Body.String())
	st.SetSetting(t.Context(), "onboarded", "1")
	check("/", authedGet(t, srv, st, "/").Body.String())
}
