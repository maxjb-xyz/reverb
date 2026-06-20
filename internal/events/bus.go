package events

import "sync"

type Event struct {
	Topic   string
	Payload any
}

type subscriber struct {
	topic string
	ch    chan Event
}

type Bus struct {
	mu   sync.Mutex
	subs map[int]*subscriber
	next int
}

func New() *Bus { return &Bus{subs: map[int]*subscriber{}} }

// Subscribe returns a buffered channel for a topic and an unsubscribe func.
func (b *Bus) Subscribe(topic string) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	s := &subscriber{topic: topic, ch: make(chan Event, 32)}
	b.subs[id] = s
	return s.ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if cur, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(cur.ch)
		}
	}
}

// Publish delivers to matching subscribers without blocking the caller.
func (b *Bus) Publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subs {
		if s.topic != ev.Topic {
			continue
		}
		select {
		case s.ch <- ev:
		default: // drop if subscriber is full; progress is coalescible
		}
	}
}
