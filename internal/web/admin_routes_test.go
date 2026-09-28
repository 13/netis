package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"netis/internal/store"
)

// route is one registration read out of the package source: its mux pattern
// and whether the handler is wrapped in requireAdmin.
type route struct {
	method, path string
	admin        bool
}

// registeredRoutes parses every non-test source file in the package and
// returns each s.mux.HandleFunc / s.mux.Handle registration. The mux cannot be
// asked for its patterns, so reading the source is what lets the tests below
// cover a route the day it is added, without anyone remembering to list it.
func registeredRoutes(t *testing.T) []route {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var out []route
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle") {
				return true
			}
			recv, ok := sel.X.(*ast.SelectorExpr)
			if !ok || recv.Sel.Name != "mux" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s: route pattern is not a string literal; the route walk cannot see it",
					fset.Position(call.Pos()))
				return true
			}
			pattern, _ := strconv.Unquote(lit.Value)
			method, path, ok := strings.Cut(pattern, " ")
			if !ok {
				t.Errorf("%s: pattern %q has no method", fset.Position(call.Pos()), pattern)
				return true
			}
			r := route{method: method, path: path}
			if wrap, ok := call.Args[1].(*ast.CallExpr); ok {
				if ws, ok := wrap.Fun.(*ast.SelectorExpr); ok && ws.Sel.Name == "requireAdmin" {
					r.admin = true
				}
			}
			out = append(out, r)
			return true
		})
	}
	if len(out) < 20 {
		t.Fatalf("found only %d routes; the source walk is broken", len(out))
	}
	return out
}

// concrete fills a pattern's wildcards with values that match.
func (r route) concrete() string {
	p := strings.ReplaceAll(r.path, "{$}", "")
	p = strings.ReplaceAll(p, "{name}", "proxmox")
	for strings.Contains(p, "{") {
		i := strings.Index(p, "{")
		j := strings.Index(p[i:], "}")
		p = p[:i] + "1" + p[i+j+1:]
	}
	return p
}

// selfService lists the state-changing routes that are deliberately open to
// every role (or to nobody logged in at all). A new mutating route that is
// neither wrapped in requireAdmin nor listed here fails the walk, so opening a
// route to viewers is always a visible decision.
var selfService = map[string]bool{
	"POST /login":                           true,
	"POST /logout":                          true,
	"POST /setup":                           true,
	"POST /settings/password":               true,
	"POST /settings/sessions/{id}/delete":   true,
	"POST /settings/sessions/revoke-others": true,
	// API tokens are per-account like sessions; the revoke handler scopes a
	// non-admin to their own tokens (TestTokenRevokeScopedToOwner).
	"POST /settings/tokens":             true,
	"POST /settings/tokens/{id}/delete": true,
}

// The parse must agree with the mux: every route it found must resolve to
// exactly that pattern, or the walk below would be testing the wrong thing.
func TestRegisteredRoutesMatchMux(t *testing.T) {
	srv, _ := testServer(t)
	for _, r := range registeredRoutes(t) {
		req := httptest.NewRequest(r.method, r.concrete(), nil)
		if _, got := srv.mux.Handler(req); got != r.method+" "+r.path {
			t.Errorf("%s %s resolves to pattern %q", r.method, r.concrete(), got)
		}
	}
}

// Every mutating route is admin-only unless it is on the self-service list.
func TestMutatingRoutesAreAdminOnly(t *testing.T) {
	for _, r := range registeredRoutes(t) {
		key := r.method + " " + r.path
		if !isMutating(r.method) {
			if r.admin {
				t.Errorf("%s: a read-only route wrapped in requireAdmin; viewers should be able to read", key)
			}
			continue
		}
		if !r.admin && !selfService[key] {
			t.Errorf("%s is open to viewers: wrap it in requireAdmin or add it to selfService", key)
		}
		if r.admin && selfService[key] {
			t.Errorf("%s is listed as self-service but wrapped in requireAdmin", key)
		}
	}
}

// viewerSession creates the admin, a viewer and the viewer's session, marks
// onboarding done, and returns the viewer's cookie.
func viewerSession(t *testing.T, st *store.Store) *http.Cookie {
	t.Helper()
	st.SetSetting(t.Context(), "onboarded", "1")
	addAdmin(t, st)
	id, err := st.CreateUser(t.Context(), "eve", "h", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(t.Context(), "viewertok", id, time.Now().Add(time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "netis_session", Value: "viewertok"}
}

// A viewer posting to any admin route gets 403 — the handler never runs —
// whether it signs in with a cookie or an API token, and an anonymous client
// is sent to the login page (or, on the API, told 401) before it gets that far.
func TestAdminRoutesRefuseViewersAndAnonymous(t *testing.T) {
	srv, st := testServer(t)
	cookie := viewerSession(t, st)
	devID, snID := seedInventory(t, st)
	eve, _, _ := st.GetUserByName(t.Context(), "eve")
	if _, err := st.CreateAPIToken(t.Context(), eve.ID, "eve", "netis_viewer", "", time.Now()); err != nil {
		t.Fatal(err)
	}

	for _, r := range registeredRoutes(t) {
		if !r.admin {
			continue
		}
		path := r.concrete()
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			req := httptest.NewRequest(r.method, path, strings.NewReader("name=x&cidr=10.9.0.0/24&kind=lan"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("viewer: code=%d, want 403", rec.Code)
			}

			if forMachines(path) {
				req = httptest.NewRequest(r.method, path, strings.NewReader(`{"name":"x","kind":"other"}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer netis_viewer")
				rec = httptest.NewRecorder()
				srv.Handler().ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Errorf("viewer token: code=%d, want 403", rec.Code)
				}
			}

			req = httptest.NewRequest(r.method, path, nil)
			rec = httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if forMachines(path) {
				if rec.Code != http.StatusUnauthorized {
					t.Errorf("anonymous: code=%d, want 401", rec.Code)
				}
				return
			}
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
				t.Errorf("anonymous: code=%d location=%q, want 303 to /login",
					rec.Code, rec.Header().Get("Location"))
			}
		})
	}

	// Nothing the viewer posted landed.
	if _, err := st.GetDevice(t.Context(), devID); err != nil {
		t.Errorf("device gone after viewer posts: %v", err)
	}
	if _, err := st.GetSubnet(t.Context(), snID); err != nil {
		t.Errorf("subnet gone after viewer posts: %v", err)
	}
	if subnets, _ := st.ListSubnets(t.Context()); len(subnets) != 1 {
		t.Errorf("subnets = %d after viewer posts, want 1", len(subnets))
	}
}
