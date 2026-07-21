package events

import (
	"context"
	"log/slog"

	"netis/internal/store"
)

type Service struct {
	store  *store.Store
	broker *Broker
}

func NewService(st *store.Store, b *Broker) *Service {
	return &Service{store: st, broker: b}
}

func (s *Service) Emit(ctx context.Context, typ string, deviceID *int64, details string) {
	if _, err := s.store.AddEvent(ctx, typ, deviceID, details); err != nil {
		slog.Error("event write failed", "err", err)
		return
	}
	s.broker.Publish("events", details)
}

func (s *Service) Broker() *Broker { return s.broker }
