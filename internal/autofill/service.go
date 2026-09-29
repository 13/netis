package autofill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"netis/internal/store"
)

// Service runs autofill passes: it recomputes the local hints (oui,
// hostname, ports) for devices, resolves every hint, and applies the result.
type Service struct {
	st       *store.Store
	kick     chan struct{}
	interval time.Duration // least time between kicked passes
	// afterPass is a test hook, called after each pass Start runs.
	afterPass func()
	// mu serialises Run: Start's background pass and a direct Run from the
	// web port-scan handler can overlap, and interleaved ReplaceHints calls
	// for the same device can hit a primary-key conflict on Postgres.
	mu sync.Mutex
}

// New returns a service over st. Call Start to serve Kick.
func New(st *store.Store) *Service {
	return &Service{st: st, kick: make(chan struct{}, 1), interval: 10 * time.Second}
}

// Enabled reports whether the admin left autofill on (the default).
func Enabled(ctx context.Context, st *store.Store) bool {
	v, err := st.GetSetting(ctx, "autofill_enabled")
	if err != nil {
		slog.Error("reading autofill_enabled failed", "err", err)
		return false
	}
	return v != "off"
}

// Kick asks for a full pass without waiting. Kicks during a pass coalesce
// into one more pass.
func (s *Service) Kick() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Start runs a full pass now, then one per burst of kicks, at most once per
// interval, until ctx is done.
func (s *Service) Start(ctx context.Context) {
	for {
		if err := s.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("autofill pass failed", "err", err)
		}
		if s.afterPass != nil {
			s.afterPass()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.interval):
		}
		select {
		case <-ctx.Done():
			return
		case <-s.kick:
		}
	}
}

// Run fills in the given devices, or every device when none are given. An
// error on one device is logged and the rest still run; the first error is
// returned.
func (s *Service) Run(ctx context.Context, ids ...int64) error {
	if !Enabled(ctx, s.st) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ids) == 0 {
		all, err := s.st.DeviceIDs(ctx)
		if err != nil {
			return err
		}
		ids = all
	}
	var first error
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.runOne(ctx, id); err != nil {
			slog.Error("autofill device failed", "device_id", id, "err", err)
			if first == nil {
				first = err
			}
		}
	}
	return first
}

func (s *Service) runOne(ctx context.Context, id int64) error {
	d, err := s.st.GetDevice(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	ifaces, err := s.st.ListIfaces(ctx, id)
	if err != nil {
		return err
	}
	var macs []string
	names := []named{{Label: "name", Value: d.Name}}
	var ports []int
	for _, f := range ifaces {
		if f.MAC != nil && *f.MAC != "" {
			macs = append(macs, *f.MAC)
		}
		if f.Hostname != nil && *f.Hostname != "" {
			names = append(names, named{Label: "hostname", Value: *f.Hostname})
		}
		ops, err := s.st.ListOpenPorts(ctx, f.ID)
		if err != nil {
			return err
		}
		for _, p := range ops {
			ports = append(ports, p.Port)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, src := range []struct {
		name  string
		hints []store.Hint
	}{
		{"oui", ouiHints(macs)},
		{"hostname", hostnameHints(names)},
		{"ports", portHints(ports)},
	} {
		for i := range src.hints {
			src.hints[i].DeviceID, src.hints[i].Source, src.hints[i].SeenAt = id, src.name, now
		}
		if err := s.st.ReplaceHints(ctx, id, src.name, src.hints); err != nil {
			return err
		}
	}

	hints, err := s.st.ListHints(ctx, id)
	if err != nil {
		return err
	}
	tagRows, err := s.st.DeviceTags(ctx, id)
	if err != nil {
		return err
	}
	tags := make([]string, len(tagRows))
	for i, t := range tagRows {
		tags[i] = t.Name
	}
	recs, err := s.st.ListAutofill(ctx, id)
	if err != nil {
		return err
	}
	fields, tagCands := Resolve(hints)
	ch := decide(d, tags, recs, fields, tagCands)
	if ch.Empty() {
		return nil
	}
	writes, added, err := s.st.ApplyAutofill(ctx, id, ch)
	if err != nil {
		return err
	}
	if len(writes) == 0 && len(added) == 0 {
		return nil
	}
	return s.st.AddAudit(ctx, store.AuditEntry{
		Username: "netis", Action: "device.autofill",
		Target: fmt.Sprintf("device %d", id), Detail: auditDetail(writes, added), Status: 200,
	})
}

// auditDetail says what a pass changed and from where:
// "kind=printer, vendor=Brother, +tag nas (hostname, oui)".
func auditDetail(writes []store.AutofillWrite, tags []store.AutofillTag) string {
	var parts []string
	srcs := map[string]bool{}
	for _, w := range writes {
		parts = append(parts, w.Field+"="+w.Value)
		srcs[w.Source] = true
	}
	for _, t := range tags {
		parts = append(parts, "+tag "+t.Name)
		srcs[t.Source] = true
	}
	var ss []string
	for s := range srcs {
		ss = append(ss, s)
	}
	sort.Strings(ss)
	return strings.Join(parts, ", ") + " (" + strings.Join(ss, ", ") + ")"
}
