package events

import "sync"

type Msg struct {
	Topic string
	Data  string
}

type Broker struct {
	mu   sync.Mutex
	subs map[chan Msg]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[chan Msg]struct{})}
}

func (b *Broker) Subscribe() (<-chan Msg, func()) {
	ch := make(chan Msg, 16)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
	return ch, cancel
}

func (b *Broker) Publish(topic, data string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- Msg{Topic: topic, Data: data}:
		default: // slow subscriber: drop
		}
	}
}
