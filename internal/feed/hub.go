// Package feed implements a simple in-process pub/sub hub used to broadcast SSE events
// (new items, pin toggles, deletes) to all connected HTTP clients.
package feed

import (
	"sync"
)

// Event is a single SSE message. Name is the event name (item.new / item.pin / item.delete),
// Data is a pre-marshaled JSON payload produced by the caller.
type Event struct {
	Name string
	Data []byte
}

// Hub is a fan-out broadcaster with a small replay ring for late-joining subscribers.
type Hub struct {
	mu         sync.Mutex
	subs       map[chan Event]struct{}
	history    []Event
	historyCap int
}

const defaultHistoryCap = 10
const subscriberBuf = 16

// NewHub returns a ready-to-use Hub.
func NewHub() *Hub {
	return &Hub{
		subs:       make(map[chan Event]struct{}),
		historyCap: defaultHistoryCap,
	}
}

// Subscribe returns a channel that receives all published events, plus a replay of the
// last `historyCap` events. The returned cancel function must be called to free resources
// (ideally via defer) — it closes the channel and removes the subscription.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuf)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	// Snapshot history under the lock so publishes that occur between unlock and replay
	// will also be delivered to ch via the normal Publish path — no missed events.
	replay := append([]Event(nil), h.history...)
	h.mu.Unlock()

	// Replay best-effort; drop any events that don't fit in the buffer (the buffer is sized
	// equal to historyCap+slop, so under normal circumstances this doesn't lose anything).
	for _, ev := range replay {
		select {
		case ch <- ev:
		default:
		}
	}

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
	return ch, cancel
}

// Publish broadcasts ev to all current subscribers (non-blocking; slow subscribers are
// dropped for this event) and records ev in the bounded replay ring.
func (h *Hub) Publish(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.history = append(h.history, ev)
	if len(h.history) > h.historyCap {
		h.history = h.history[len(h.history)-h.historyCap:]
	}

	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
			// Slow or disconnected subscriber; drop this event. They still have history
			// available via a fresh Subscribe if they reconnect.
		}
	}
}
