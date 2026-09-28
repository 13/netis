package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"netis/internal/events"
	"netis/internal/store"
)

// request is what a fake endpoint received.
type request struct {
	header http.Header
	body   string
}

// endpoint starts a server that records every request and answers with the
// statuses given, in turn, then 200.
func endpoint(t *testing.T, statuses ...int) (*httptest.Server, chan request, *atomic.Int32) {
	t.Helper()
	got := make(chan request, 100)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		n := int(calls.Add(1))
		got <- request{header: r.Header.Clone(), body: string(b)}
		if n <= len(statuses) {
			w.WriteHeader(statuses[n-1])
		}
	}))
	t.Cleanup(srv.Close)
	return srv, got, &calls
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func set(t *testing.T, st *store.Store, kv ...string) {
	t.Helper()
	for i := 0; i < len(kv); i += 2 {
		if err := st.SetSetting(t.Context(), kv[i], kv[i+1]); err != nil {
			t.Fatal(err)
		}
	}
}

// running starts a notifier with a short batch window and no retry delay.
func running(t *testing.T, st *store.Store, window time.Duration) *Notifier {
	t.Helper()
	n := New(st)
	n.window = window
	n.send.backoff = []time.Duration{time.Millisecond, time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { n.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return n
}

func next(t *testing.T, ch chan request) request {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("no request arrived")
	}
	return request{}
}

func none(t *testing.T, ch chan request, wait time.Duration) {
	t.Helper()
	select {
	case r := <-ch:
		t.Fatalf("unexpected request: %s", r.body)
	case <-time.After(wait):
	}
}

func device(t *testing.T, st *store.Store, name string, alert bool) int64 {
	t.Helper()
	id, err := st.CreateDevice(t.Context(), store.Device{Name: name, Kind: "server", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetDeviceAlertOffline(t.Context(), id, alert); err != nil {
		t.Fatal(err)
	}
	return id
}

// A burst of events inside the window goes out as one summary.
func TestBurstIsCoalescedIntoOneMessage(t *testing.T) {
	st := openStore(t)
	srv, got, _ := endpoint(t)
	set(t, st, KeyWebhookURL, srv.URL, KeyBaseURL, "https://netis.lan/")
	// The window has to outlast filtering all 50 events, which looks each one
	// up in the store and is slow under -race.
	n := running(t, st, 2*time.Second)

	for i := 0; i < 50; i++ {
		n.Notify("device_new", nil, "new device at 10.0.0."+string(rune('a'+i%26)))
	}
	var p webhookEvent
	if err := json.Unmarshal([]byte(next(t, got).body), &p); err != nil {
		t.Fatal(err)
	}
	if p.Event != "batch" || len(p.Events) != 50 || p.Details != "50 new devices" {
		t.Fatalf("batch = event %q, %d events, details %q", p.Event, len(p.Events), p.Details)
	}
	if p.URL != "https://netis.lan/events" {
		t.Fatalf("batch url = %q", p.URL)
	}
	none(t, got, 400*time.Millisecond)
}

func TestWebhookSingleEventPayload(t *testing.T) {
	st := openStore(t)
	srv, got, _ := endpoint(t)
	set(t, st, KeyWebhookURL, srv.URL, KeyWebhookAuth, "Token abc", KeyBaseURL, "https://netis.lan")
	id := device(t, st, "nas", true)
	n := running(t, st, 10*time.Millisecond)

	n.Notify("offline", &id, "nas (10.0.0.5) went offline")
	r := next(t, got)
	if r.header.Get("Authorization") != "Token abc" || r.header.Get("Content-Type") != "application/json" {
		t.Fatalf("headers: %v", r.header)
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(r.body), &p); err != nil {
		t.Fatal(err)
	}
	dev, _ := p["device"].(map[string]any)
	if p["event"] != "offline" || p["details"] != "nas (10.0.0.5) went offline" ||
		dev["name"] != "nas" || dev["id"] != float64(id) ||
		p["url"] != "https://netis.lan/devices/"+jsonNum(id) {
		t.Fatalf("payload = %s", r.body)
	}
	if _, err := time.Parse(time.RFC3339, p["time"].(string)); err != nil {
		t.Fatalf("time %v: %v", p["time"], err)
	}
}

func jsonNum(id int64) string {
	b, _ := json.Marshal(id)
	return string(b)
}

func TestNtfyRequest(t *testing.T) {
	st := openStore(t)
	srv, got, _ := endpoint(t)
	set(t, st, KeyNtfyURL, srv.URL+"/netis-alerts", KeyNtfyToken, "tk_123", KeyBaseURL, "https://netis.lan")
	n := running(t, st, 10*time.Millisecond)

	n.Notify("ip_conflict", nil, "10.0.0.5 claimed by alpha, bravo")
	r := next(t, got)
	if r.body != "10.0.0.5 claimed by alpha, bravo" {
		t.Fatalf("body = %q", r.body)
	}
	want := map[string]string{
		"Title": "netis: IP conflict", "Priority": "4", "Tags": "warning",
		"Click": "https://netis.lan/events", "Authorization": "Bearer tk_123",
	}
	for k, v := range want {
		if got := r.header.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

// Without a token or base URL, ntfy gets neither header.
func TestNtfyOmitsUnsetHeaders(t *testing.T) {
	st := openStore(t)
	srv, got, _ := endpoint(t)
	set(t, st, KeyNtfyURL, srv.URL)
	n := running(t, st, 10*time.Millisecond)
	n.Notify("device_new", nil, "new device")
	r := next(t, got)
	if r.header.Get("Authorization") != "" || r.header.Get("Click") != "" {
		t.Fatalf("headers: %v", r.header)
	}
}

func TestFilteringByTypeAndDeviceFlag(t *testing.T) {
	st := openStore(t)
	srv, got, _ := endpoint(t)
	set(t, st, KeyWebhookURL, srv.URL, "notify_device_new", "0")
	watched := device(t, st, "nas", true)
	ignored := device(t, st, "phone", false)
	n := running(t, st, 100*time.Millisecond)

	n.Notify("offline", &ignored, "phone went offline") // flag off
	n.Notify("device_new", nil, "new device")           // group off
	n.Notify("ip_changed", &watched, "moved")           // never sent
	n.Notify("online", &watched, "nas is online")       // sent

	var p webhookEvent
	if err := json.Unmarshal([]byte(next(t, got).body), &p); err != nil {
		t.Fatal(err)
	}
	if p.Event != "online" || p.Details != "nas is online" {
		t.Fatalf("sent %+v, want only the watched device's online event", p)
	}
	none(t, got, 300*time.Millisecond)
}

// With no channel configured nothing is attempted.
func TestNoChannelSendsNothing(t *testing.T) {
	st := openStore(t)
	n := New(st)
	if _, ok := n.accept(t.Context(), raw{typ: "device_new"}); ok {
		t.Fatal("event accepted with no channel configured")
	}
}

// Emit must return promptly even when the notifier is not draining its queue.
func TestFullQueueDoesNotBlockEmit(t *testing.T) {
	st := openStore(t)
	n := New(st) // not running: nothing drains the queue
	svc := events.NewService(st, events.NewBroker())
	svc.SetSubscriber(n)

	done := make(chan struct{})
	go func() {
		for i := 0; i < queueSize+50; i++ {
			svc.Emit(context.Background(), "device_new", nil, "x")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Emit blocked on a full notification queue")
	}
	if len(n.queue) != queueSize {
		t.Fatalf("queue holds %d, want %d", len(n.queue), queueSize)
	}
}

func TestRetriesServerErrorsButNotClientErrors(t *testing.T) {
	srv, _, calls := endpoint(t, 500, 503)
	s := newSender()
	s.backoff = []time.Duration{time.Millisecond, time.Millisecond}
	c := Config{WebhookURL: srv.URL}
	if err := s.deliver(t.Context(), c, testMessage(c)); err != nil {
		t.Fatalf("deliver after two 5xx: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("attempts = %d, want 3", calls.Load())
	}

	srv2, _, calls2 := endpoint(t, 401, 401, 401)
	c = Config{WebhookURL: srv2.URL}
	err := s.deliver(t.Context(), c, testMessage(c))
	if err == nil || !strings.Contains(err.Error(), "webhook: HTTP 401") {
		t.Fatalf("err = %v", err)
	}
	if calls2.Load() != 1 {
		t.Fatalf("attempts on 401 = %d, want 1", calls2.Load())
	}
}

func TestSendTest(t *testing.T) {
	st := openStore(t)
	if err := SendTest(t.Context(), st); err == nil {
		t.Fatal("SendTest with no channel returned nil")
	}
	srv, got, _ := endpoint(t)
	set(t, st, KeyWebhookURL, srv.URL)
	if err := SendTest(t.Context(), st); err != nil {
		t.Fatal(err)
	}
	if r := next(t, got); !strings.Contains(r.body, `"event":"test"`) {
		t.Fatalf("body = %s", r.body)
	}
}

func TestSummaryListsAtMostMaxLines(t *testing.T) {
	var items []Item
	for i := 0; i < maxLines+5; i++ {
		items = append(items, Item{Type: "offline", Details: "x"})
	}
	items = append(items, Item{Type: "device_new", Details: "y"})
	m := buildMessage(Config{}, items)
	if m.summaryLine() != "1 new device, 25 went offline" {
		t.Fatalf("summary = %q", m.summaryLine())
	}
	if !strings.HasSuffix(m.Body, "… and 6 more") || m.Priority != 4 {
		t.Fatalf("body tail / priority: %q %d", m.Body[len(m.Body)-20:], m.Priority)
	}
}

func TestValidURL(t *testing.T) {
	for s, ok := range map[string]bool{
		"": true, "https://ntfy.sh/x": true, "http://10.0.0.2:8080/hook": true,
		"ftp://x": false, "ntfy.sh/x": false, "https://": false,
	} {
		if err := ValidURL(s); (err == nil) != ok {
			t.Errorf("ValidURL(%q) = %v", s, err)
		}
	}
}

// A sweep failing every couple of minutes is one message, not one per sweep.
func TestRepeatedScanErrorIsQuietened(t *testing.T) {
	st := openStore(t)
	srv, _, _ := endpoint(t)
	set(t, st, KeyWebhookURL, srv.URL)
	n := New(st)
	at := time.Now()
	accept := func(details string, at time.Time) bool {
		_, ok := n.accept(t.Context(), raw{typ: "scan_error", details: details, at: at})
		return ok
	}
	if !accept("subnet 10.0.0.0/24: boom", at) {
		t.Fatal("first error not accepted")
	}
	if accept("subnet 10.0.0.0/24: boom", at.Add(2*time.Minute)) {
		t.Fatal("repeat within the hour accepted")
	}
	if !accept("subnet 10.0.1.0/24: boom", at.Add(2*time.Minute)) {
		t.Fatal("a different error was quietened")
	}
	if !accept("subnet 10.0.0.0/24: boom", at.Add(repeatQuiet+time.Minute)) {
		t.Fatal("repeat after the quiet period not accepted")
	}
}
