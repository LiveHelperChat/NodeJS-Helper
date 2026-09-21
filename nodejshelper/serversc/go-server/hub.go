package main

import (
	"encoding/json"
	"sync"
)

// Hub keeps the channel -> sockets mapping. It replicates what the
// SCSimpleBroker (worker side) plus sc-simple-broker exchange used to do:
// every socket subscribed to a channel receives a `#publish` packet, the
// sender included when it is subscribed itself.
type Hub struct {
	mu   sync.RWMutex
	subs map[string]map[*Socket]struct{}

	// Hooks used by the Redis bridge to mirror sc-redis' behaviour of
	// (un)subscribing Redis channels together with the SC channels.
	onFirstSubscribe  func(channel string)
	onLastUnsubscribe func(channel string)
}

func newHub() *Hub {
	return &Hub{subs: make(map[string]map[*Socket]struct{})}
}

// Count returns how many local sockets are subscribed to a channel.
func (h *Hub) Count(channel string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[channel])
}

// Add registers the socket on the channel. It reports whether the socket was
// the first local subscriber (which is when Redis has to be subscribed too).
func (h *Hub) Add(s *Socket, channel string) (first bool) {
	h.mu.Lock()
	set, ok := h.subs[channel]
	if !ok {
		set = make(map[*Socket]struct{})
		h.subs[channel] = set
		first = true
	}
	set[s] = struct{}{}
	h.mu.Unlock()

	if first && h.onFirstSubscribe != nil {
		h.onFirstSubscribe(channel)
	}
	return first
}

// Remove drops the socket from the channel. It reports whether the channel ran
// out of local subscribers (which is when Redis can be unsubscribed).
func (h *Hub) Remove(s *Socket, channel string) (last bool) {
	h.mu.Lock()
	if set, ok := h.subs[channel]; ok {
		if _, exists := set[s]; exists {
			delete(set, s)
			if len(set) == 0 {
				delete(h.subs, channel)
				last = true
			}
		}
	}
	h.mu.Unlock()

	if last && h.onLastUnsubscribe != nil {
		h.onLastUnsubscribe(channel)
	}
	return last
}

// PublishLocal delivers a `#publish` event to every local subscriber.
func (h *Hub) PublishLocal(channel string, data json.RawMessage) {
	h.mu.RLock()
	set := h.subs[channel]
	targets := make([]*Socket, 0, len(set))
	for s := range set {
		targets = append(targets, s)
	}
	h.mu.RUnlock()

	for _, s := range targets {
		s.SendPublish(channel, data)
	}
}
