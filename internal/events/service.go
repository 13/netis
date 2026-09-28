package events

import (
	"context"
	"log/slog"

	"netis/internal/store"
)

// Subscriber is told about every event after it has been recorded. Notify is
// called on the emitting goroutine — a scan or an integration sync — so it
// must return at once and do any slow work elsewhere.
type Subscriber interface {
	Notify(typ string, deviceID *int64, details string)
}

type Service struct {
	store  *store.Store
	broker *Broker
	sub    Subscriber
}

func NewService(st *store.Store, b *Broker) *Service {
	return &Service{store: st, broker: b}
}

// SetSubscriber registers the one subscriber (the notifier) that hears about
// each recorded event. Call it before anything emits.
func (s *Service) SetSubscriber(sub Subscriber) { s.sub = sub }

func (s *Service) Emit(ctx context.Context, typ string, deviceID *int64, details string) {
	if _, err := s.store.AddEvent(ctx, typ, deviceID, details); err != nil {
		slog.Error("event write failed", "err", err)
		return
	}
	s.broker.Publish("events", details)
	if s.sub != nil {
		s.sub.Notify(typ, deviceID, details)
	}
}

func (s *Service) Broker() *Broker { return s.broker }
