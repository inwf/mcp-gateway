// Package events carries notifications between subsystems: an upstream
// server changing state, a configuration being updated, a tool call
// finishing.
//
// The bus exists so that the WebSocket layer can watch what happens
// without the layers that make things happen having to know about it.
package events

import (
	"sync"
	"sync/atomic"
	"time"
)

// Kind names what happened. The values are the same strings the
// WebSocket API sends, so a reader of either can match them up.
type Kind string

const (
	ServerConnected    Kind = "server.connected"
	ServerDisconnected Kind = "server.disconnected"
	ServerStatus       Kind = "server.status"
	ServerFailed       Kind = "server.failed"

	ToolsChanged     Kind = "tools.changed"
	ResourcesChanged Kind = "resources.changed"

	ConfigUpdated Kind = "config.updated"

	ToolCallStarted   Kind = "toolcall.started"
	ToolCallCompleted Kind = "toolcall.completed"
	ToolCallFailed    Kind = "toolcall.failed"
)

// Kinds lists every kind, for a client that wants to name what it is
// unsubscribing from and for validating a subscription request.
var Kinds = []Kind{
	ServerConnected,
	ServerDisconnected,
	ServerStatus,
	ServerFailed,
	ToolsChanged,
	ResourcesChanged,
	ConfigUpdated,
	ToolCallStarted,
	ToolCallCompleted,
	ToolCallFailed,
}

// Event is one notification.
type Event struct {
	Kind Kind      `json:"kind"`
	At   time.Time `json:"at"`

	// Server names the upstream server this concerns, when it concerns
	// one.
	Server string `json:"server,omitempty"`

	// Data carries whatever the subscriber needs, and is shared between
	// subscribers: it must not be modified after publishing.
	Data any `json:"data,omitempty"`
}

// subscriberBuffer is how many events a subscriber may fall behind by
// before it starts missing them. Generous enough to absorb a burst of
// upstream churn, small enough that a dead subscriber does not pin much
// memory.
const subscriberBuffer = 64

type subscriber struct {
	kinds map[Kind]bool // nil means every kind
	ch    chan Event
}

// Bus delivers events to any number of subscribers.
//
// Publishing never blocks. A subscriber that stops reading loses events
// rather than stalling the subsystem that published them — a log viewer
// nobody is watching must not be able to hold up a tool call.
type Bus struct {
	mu     sync.RWMutex
	subs   map[int]*subscriber
	nextID int
	closed bool

	dropped atomic.Int64

	// Now overrides the clock, for tests that assert on timestamps.
	Now func() time.Time
}

func NewBus() *Bus {
	return &Bus{subs: map[int]*subscriber{}}
}

// Publish delivers e to every interested subscriber.
func (b *Bus) Publish(e Event) {
	if e.At.IsZero() {
		e.At = b.now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}
	for _, sub := range b.subs {
		if sub.kinds != nil && !sub.kinds[e.Kind] {
			continue
		}
		select {
		case sub.ch <- e:
		default:
			// The subscriber is behind. Counting the loss is what makes
			// it diagnosable rather than mysterious.
			b.dropped.Add(1)
		}
	}
}

// PublishServer is the common case: something happened to one server.
func (b *Bus) PublishServer(kind Kind, server string, data any) {
	b.Publish(Event{Kind: kind, Server: server, Data: data})
}

// Subscribe returns a channel of events and a function that cancels the
// subscription. With no kinds given, every event is delivered.
//
// The cancel function must be called, or the subscription leaks. It is
// safe to call more than once.
func (b *Bus) Subscribe(kinds ...Kind) (<-chan Event, func()) {
	var wanted map[Kind]bool
	if len(kinds) > 0 {
		wanted = make(map[Kind]bool, len(kinds))
		for _, k := range kinds {
			wanted[k] = true
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, subscriberBuffer)
	if b.closed {
		close(ch)
		return ch, func() {}
	}

	id := b.nextID
	b.nextID++
	b.subs[id] = &subscriber{kinds: wanted, ch: ch}

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if sub, ok := b.subs[id]; ok {
				delete(b.subs, id)
				close(sub.ch)
			}
		})
	}
}

// Dropped reports how many deliveries were skipped because a subscriber
// was not keeping up. A number that climbs means a subscriber is stuck.
func (b *Bus) Dropped() int64 { return b.dropped.Load() }

// Subscribers reports how many subscriptions are active.
func (b *Bus) Subscribers() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}

// Close ends every subscription. Publishing afterwards is a no-op, so
// that a subsystem shutting down after the bus does not panic.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true
	for id, sub := range b.subs {
		delete(b.subs, id)
		close(sub.ch)
	}
}

func (b *Bus) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}
