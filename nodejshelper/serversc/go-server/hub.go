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

	// targetsPool recycles the subscriber snapshots PublishLocal takes, so a hot
	// channel does not allocate an N sized slice on every single publish.
	targetsPool sync.Pool

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
//
// The packet is encoded once and the very same byte slice is queued for every
// subscriber: the bytes are identical for all of them. Previously SendPublish
// was called per socket, which json.Marshal'ed the payload and then the envelope
// again for each of them - on a busy channel that dominated the CPU profile
// (encoding/json ~12%, appendCompact alone 7.5%).
func (h *Hub) PublishLocal(channel string, data json.RawMessage) {
	h.mu.RLock()
	set := h.subs[channel]
	if len(set) == 0 {
		h.mu.RUnlock()
		return
	}

	targets, _ := h.targetsPool.Get().([]*Socket)
	targets = targets[:0]
	for s := range set {
		targets = append(targets, s)
	}
	h.mu.RUnlock()

	packet := encodePublish(channel, data)

	if packet != nil {
		for _, s := range targets {
			// Closed sockets are still in the map until cleanup runs; skipping
			// them avoids pointless work during a burst of disconnects.
			if s.closed.Load() {
				continue
			}
			s.enqueueText(packet)
		}
	}

	// Do not keep the sockets reachable through the pool.
	for i := range targets {
		targets[i] = nil
	}
	h.targetsPool.Put(targets[:0])
}

// outPublishEvent is the `#publish` packet envelope. It exists so that the whole
// packet can be produced by a single json.Marshal call.
type outPublishEvent struct {
	Event string     `json:"event"`
	Data  outPublish `json:"data"`
}

// encodePublish builds the wire bytes of one `#publish` packet. The result is
// shared by every subscriber of the channel and must be treated as read only.
// It returns nil when the packet cannot be encoded, in which case there is
// nothing sensible to deliver.
func encodePublish(channel string, data json.RawMessage) []byte {
	if data == nil {
		data = json.RawMessage("null")
	}

	packet, err := json.Marshal(outPublishEvent{
		Event: "#publish",
		Data:  outPublish{Channel: channel, Data: data},
	})
	if err != nil {
		logErrorf("[hub] failed to encode #publish for channel %q: %v", channel, err)
		return nil
	}

	return packet
}
