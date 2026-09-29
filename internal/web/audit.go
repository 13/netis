package web

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"netis/internal/store"
	"netis/internal/web/views"
)

// auditActions names the mutating routes in the audit log. A route missing
// from the table is still recorded, under its pattern, so a route added later
// is audited from the day it exists; adding it here only gives it a tidier
// name.
var auditActions = map[string]string{
	"POST /login":                            "login",
	"POST /logout":                           "logout",
	"POST /setup":                            "setup",
	"POST /welcome/subnets":                  "welcome.subnets",
	"POST /welcome/integrations":             "welcome.integrations",
	"POST /welcome/skip":                     "welcome.skip",
	"POST /scan":                             "scan.all",
	"POST /subnets/{id}/scan":                "scan.subnet",
	"POST /subnets/{id}/cell":                "ip.kind",
	"POST /devices":                          "device.create",
	"POST /devices/{id}":                     "device.update",
	"POST /devices/{id}/delete":              "device.delete",
	"POST /devices/{id}/approve":             "device.approve",
	"POST /devices/bulk/approve":             "device.bulk_approve",
	"POST /devices/bulk/tag":                 "device.bulk_tag",
	"POST /devices/bulk/delete":              "device.bulk_delete",
	"POST /devices/{id}/links":               "link.add",
	"POST /links/{id}/delete":                "link.delete",
	"POST /devices/{id}/fields":              "field.set",
	"POST /devices/{id}/fields/delete":       "field.delete",
	"POST /devices/{id}/wol":                 "device.wol",
	"POST /devices/{id}/portscan":            "device.portscan",
	"POST /devices/{id}/ip/kind":             "ip.kind",
	"POST /devices/{id}/alert":               "device.alert",
	"POST /settings/notifications":           "notifications.save",
	"POST /settings/notifications/test":      "notifications.test",
	"POST /settings/subnets":                 "subnet.create",
	"POST /settings/subnets/{id}":            "subnet.update",
	"POST /settings/subnets/{id}/delete":     "subnet.delete",
	"POST /settings/integrations":            "integrations.save",
	"POST /settings/integrations/{name}/run": "integration.run",
	"POST /settings/users":                   "user.create",
	"POST /settings/users/{id}/delete":       "user.delete",
	"POST /settings/users/{id}/role":         "user.role",
	"POST /settings/users/{id}/password":     "user.password_reset",
	"POST /settings/password":                "password.change",
	"POST /settings/sessions/{id}/delete":    "session.revoke",
	"POST /settings/sessions/revoke-others":  "session.revoke_others",
	"POST /settings/general":                 "settings.save",
	"POST /settings/tokens":                  "token.create",
	"POST /settings/tokens/{id}/delete":      "token.revoke",
	"POST /settings/sso/link":                "sso.link_start",
	"POST /devices/import":                   "device.import",
	"POST /api/devices":                      "api.device.create",
	"PATCH /api/devices/{id}":                "api.device.update",
	"DELETE /api/devices/{id}":               "api.device.delete",
}

// auditRecord is what a handler may add to the entry the audit middleware
// writes for its request. Everything in it is written verbatim, so a handler
// must never put a password, secret or raw form value here.
type auditRecord struct {
	// Target replaces the target derived from the route's wildcard.
	Target string
	// Detail is a short free-text note: the new role, why a login failed.
	Detail string
	// UserID and Username name the actor when the request has no session
	// user yet (login, setup).
	UserID   *int64
	Username string
}

type auditKey struct{}

// auditNote returns the record the audit middleware will write for r. Outside
// the middleware (a handler called directly in a test) it returns a throwaway
// record, so callers never need to check.
func auditNote(r *http.Request) *auditRecord {
	if a, ok := r.Context().Value(auditKey{}).(*auditRecord); ok {
		return a
	}
	return &auditRecord{}
}

// statusRecorder captures the status a handler answered with.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// audit records every state-changing request that reached a registered route:
// who, what, from where, and the status it was answered with. It wraps the mux
// so r.Pattern, which the mux sets on the request in place, names the route
// once the handler returns. Reads are not recorded, and neither are requests
// the mux did not match — those never reached a handler.
func (s *Server) audit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isMutating(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		note := &auditRecord{}
		r = r.WithContext(context.WithValue(r.Context(), auditKey{}, note))
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if r.Pattern == "" {
			return
		}
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		e := store.AuditEntry{
			At: time.Now(), Action: auditAction(r.Pattern), Target: note.Target,
			Detail: note.Detail, IP: s.clientIP(r), Status: status,
			UserID: note.UserID, Username: note.Username,
		}
		if e.Target == "" {
			e.Target = patternTarget(r)
		}
		if u, ok := userFrom(r); ok {
			e.UserID, e.Username = &u.ID, u.Username
		}
		// requireAuth authenticates a bearer request by its token alone, so
		// the user above is the token's owner; say it was not their browser.
		if _, ok := apiBearer(r); ok {
			e.Detail = strings.TrimSpace("via API token " + e.Detail)
		}
		// The client may have hung up; the entry is still wanted.
		if err := s.store.AddAudit(context.WithoutCancel(r.Context()), e); err != nil {
			slog.Error("audit: record entry", "action", e.Action, "err", err)
		}
	})
}

func isMutating(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func auditAction(pattern string) string {
	if a, ok := auditActions[pattern]; ok {
		return a
	}
	return pattern
}

// patternTarget names what a request acted on from its route's first
// wildcard: "POST /devices/{id}/wol" with id 12 is "device 12". A route with
// no wildcard has no target.
func patternTarget(r *http.Request) string {
	_, path, _ := strings.Cut(r.Pattern, " ")
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for i, seg := range segs {
		if !strings.HasPrefix(seg, "{") || !strings.HasSuffix(seg, "}") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(seg, "{"), "}")
		if name == "$" {
			continue
		}
		noun := name
		if i > 0 {
			noun = strings.TrimSuffix(segs[i-1], "s")
		}
		return noun + " " + truncate(r.PathValue(name), 64)
	}
	return ""
}

// auditPageSize is how many entries one page of the Audit tab shows.
const auditPageSize = 50

// auditData loads the Audit tab from the request's filter and cursor.
func (s *Server) auditData(r *http.Request) (views.AuditData, error) {
	q := r.URL.Query()
	d := views.AuditData{User: q.Get("user"), Action: q.Get("action")}
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	d.Paged = before > 0
	var err error
	d.Entries, d.More, err = s.store.ListAudit(r.Context(), store.AuditFilter{
		Username: d.User, Action: d.Action, BeforeID: before, Limit: auditPageSize,
	})
	if err != nil {
		return d, err
	}
	if d.Targets, err = s.auditTargets(r.Context(), d.Entries); err != nil {
		return d, err
	}
	d.Users, d.Actions, err = s.store.AuditFacets(r.Context())
	return d, err
}

var (
	// auditDeviceByID matches a target recorded from a route wildcard,
	// "device 12"; auditDeviceNamed one recorded by deviceTarget.
	auditDeviceByID  = regexp.MustCompile(`^device (\d+)$`)
	auditDeviceNamed = regexp.MustCompile(`^device (.+) \(#(\d+)\)$`)
	auditSubnetByID  = regexp.MustCompile(`^subnet (\d+)$`)
)

// auditTargets resolves the entries whose target names a device or subnet by
// id to its current name and page, keyed by entry id. One that no longer
// exists is marked deleted. Other targets are left to render as recorded.
func (s *Server) auditTargets(ctx context.Context, entries []store.AuditEntry) (map[int64]views.AuditTarget, error) {
	devIDs := map[int64][]int64{}  // device id -> entry ids
	snapshot := map[int64]string{} // entry id -> name recorded with the entry
	subIDs := map[int64][]int64{}
	for _, e := range entries {
		if m := auditDeviceByID.FindStringSubmatch(e.Target); m != nil {
			id, _ := strconv.ParseInt(m[1], 10, 64)
			devIDs[id] = append(devIDs[id], e.ID)
		} else if m := auditDeviceNamed.FindStringSubmatch(e.Target); m != nil {
			id, _ := strconv.ParseInt(m[2], 10, 64)
			devIDs[id] = append(devIDs[id], e.ID)
			snapshot[e.ID] = m[1]
		} else if m := auditSubnetByID.FindStringSubmatch(e.Target); m != nil {
			id, _ := strconv.ParseInt(m[1], 10, 64)
			subIDs[id] = append(subIDs[id], e.ID)
		}
	}
	out := map[int64]views.AuditTarget{}
	if len(devIDs) > 0 {
		ids := make([]int64, 0, len(devIDs))
		for id := range devIDs {
			ids = append(ids, id)
		}
		names, err := s.store.DeviceNames(ctx, ids)
		if err != nil {
			return nil, err
		}
		for id, entryIDs := range devIDs {
			for _, eid := range entryIDs {
				if name, ok := names[id]; ok {
					out[eid] = views.AuditTarget{Text: name, Href: "/devices/" + strconv.FormatInt(id, 10)}
				} else if snap := snapshot[eid]; snap != "" {
					out[eid] = views.AuditTarget{Text: "deleted device " + snap}
				} else {
					out[eid] = views.AuditTarget{Text: "deleted device"}
				}
			}
		}
	}
	if len(subIDs) > 0 {
		subnets, err := s.store.ListSubnets(ctx)
		if err != nil {
			return nil, err
		}
		byID := map[int64]store.Subnet{}
		for _, sn := range subnets {
			byID[sn.ID] = sn
		}
		for id, entryIDs := range subIDs {
			for _, eid := range entryIDs {
				if sn, ok := byID[id]; ok {
					out[eid] = views.AuditTarget{Text: "subnet " + sn.Name, Href: "/subnets/" + strconv.FormatInt(id, 10)}
				} else {
					out[eid] = views.AuditTarget{Text: "deleted subnet"}
				}
			}
		}
	}
	return out, nil
}

// deviceTarget names a device in the audit log by name and id: the name is
// what a reader recognises, the id what survives a rename.
func deviceTarget(d store.Device) string {
	return "device " + d.Name + " (#" + strconv.FormatInt(d.ID, 10) + ")"
}
