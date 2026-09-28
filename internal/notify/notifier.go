package notify

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"netis/internal/store"
)

const (
	// queueSize bounds the events waiting to be filtered. Beyond it an event
	// is dropped, not waited for: a scan must never stall on a notification.
	queueSize = 256
	// batchWindow is how long the first event of a batch waits for company, so
	// a scan that finds fifty devices sends one message rather than fifty.
	batchWindow = 30 * time.Second
	// outSize bounds the batches waiting for the sender. It only fills while
	// an endpoint is timing out and being retried.
	outSize = 4
	// repeatQuiet is how long an identical scan_error stays unsent. A subnet
	// sweep that fails raises the event on every attempt, every couple of
	// minutes; one message per hour of outage is plenty.
	repeatQuiet = time.Hour
)

// raw is an event as emitted, before filtering.
type raw struct {
	typ      string
	deviceID *int64
	details  string
	at       time.Time
}

// Notifier is the events service's subscriber. Notify queues an event without
// blocking; Run filters queued events against the current settings, collects
// them into batches and sends each batch on a separate goroutine, so a slow
// endpoint delays nothing but its own messages.
type Notifier struct {
	st     *store.Store
	queue  chan raw
	out    chan []Item
	window time.Duration
	send   *sender
	// lastError remembers when each scan_error text was last accepted. Only
	// the collector goroutine touches it.
	lastError map[string]time.Time
}

func New(st *store.Store) *Notifier {
	return &Notifier{
		st: st, queue: make(chan raw, queueSize), out: make(chan []Item, outSize),
		window: batchWindow, send: newSender(), lastError: map[string]time.Time{},
	}
}

// Notify queues an event. It never blocks: when the queue is full the event is
// dropped and logged.
func (n *Notifier) Notify(typ string, deviceID *int64, details string) {
	select {
	case n.queue <- raw{typ: typ, deviceID: deviceID, details: details, at: time.Now().UTC()}:
	default:
		slog.Warn("notification queue full; event not sent", "type", typ, "details", details)
	}
}

// Run collects and sends until ctx is cancelled. A batch still being collected
// at shutdown is not sent.
func (n *Notifier) Run(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		n.sendLoop(ctx)
	}()
	n.collect(ctx)
	<-done
}

func (n *Notifier) collect(ctx context.Context) {
	var pending []Item
	var flush <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-n.queue:
			it, ok := n.accept(ctx, r)
			if !ok {
				continue
			}
			pending = append(pending, it)
			if flush == nil {
				flush = time.After(n.window)
			}
		case <-flush:
			flush = nil
			select {
			case n.out <- pending:
			default:
				slog.Warn("notification sender backed up; batch not sent", "events", len(pending))
			}
			pending = nil
		}
	}
}

// accept decides whether an event is to be sent under the current settings,
// and resolves its device's name while the device still exists.
func (n *Notifier) accept(ctx context.Context, r raw) (Item, bool) {
	c, err := LoadConfig(ctx, n.st)
	if err != nil {
		slog.Error("notification settings unreadable; event not sent", "type", r.typ, "err", err)
		return Item{}, false
	}
	if !c.HasChannel() || !c.Wants(r.typ) {
		return Item{}, false
	}
	if r.typ == "scan_error" {
		if last, ok := n.lastError[r.details]; ok && r.at.Sub(last) < repeatQuiet {
			return Item{}, false
		}
		if len(n.lastError) > 100 {
			for k, t := range n.lastError {
				if r.at.Sub(t) >= repeatQuiet {
					delete(n.lastError, k)
				}
			}
		}
		n.lastError[r.details] = r.at
	}
	it := Item{Type: r.typ, DeviceID: r.deviceID, Details: r.details, Time: r.at}
	if r.deviceID != nil {
		name, alert, err := n.st.DeviceAlert(ctx, *r.deviceID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			slog.Error("notification device lookup failed", "device_id", *r.deviceID, "err", err)
		}
		it.DeviceName = name
		// Phones and laptops come and go all day; only the devices someone
		// asked about are worth a message.
		if (r.typ == "offline" || r.typ == "online") && !alert {
			return Item{}, false
		}
	}
	return it, true
}

func (n *Notifier) sendLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case items := <-n.out:
			// Channels are read again at send time, so a batch collected just
			// before a settings change goes where the settings now say.
			c, err := LoadConfig(ctx, n.st)
			if err != nil {
				slog.Error("notification settings unreadable; batch not sent", "events", len(items), "err", err)
				continue
			}
			if err := n.send.deliver(ctx, c, buildMessage(c, items)); err != nil {
				slog.Warn("notification delivery failed", "events", len(items), "err", err)
			}
		}
	}
}

// SendTest sends a test message to every configured channel now, once each
// with no retries, and returns what went wrong per channel.
func SendTest(ctx context.Context, st *store.Store) error {
	c, err := LoadConfig(ctx, st)
	if err != nil {
		return err
	}
	if !c.HasChannel() {
		return errors.New("no channel configured")
	}
	s := newSender()
	s.backoff = nil
	return s.deliver(ctx, c, testMessage(c))
}
