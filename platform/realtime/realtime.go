// Package realtime provides a portable WebSocket fan-out hub abstraction.
// Donor: aodex-go/internal/ws (Hub + Connection + Redis fan-out). The
// transport (WebSocket connection itself) is consumer-owned; this package
// provides the channel/subscriber/publisher contract.
//
// See TRD 18-03.
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
)

// Channel is a logical fan-out target.
type Channel string

// Message is a single broadcast.
type Message struct {
	Channel  Channel           `json:"channel"`
	Type     string            `json:"type"`
	Data     json.RawMessage   `json:"data,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Subscriber receives messages for one channel. Implementations wrap a
// transport (websocket, sse, etc.).
type Subscriber interface {
	ID() string
	Channel() Channel
	Send(Message) error
	Close()
}

// Stats are point-in-time hub counters.
type Stats struct {
	Channels         int   `json:"channels"`
	Subscribers      int   `json:"subscribers"`
	Published        int64 `json:"published"`
	Delivered        int64 `json:"delivered"`
	Dropped          int64 `json:"dropped"`
	RemoteIngress    int64 `json:"remote_ingress"`
	RemoteEgress     int64 `json:"remote_egress"`
}

// Hub is the realtime fan-out contract.
type Hub interface {
	Subscribe(ctx context.Context, ch Channel, sub Subscriber) error
	Unsubscribe(ch Channel, sub Subscriber)
	Publish(ctx context.Context, msg Message) error
	Presence(ch Channel) []string
	Stats() Stats
	Stop()
}

// ErrHubStopped is returned when the hub has been stopped.
var ErrHubStopped = errors.New("realtime: hub stopped")

// localHub is the in-process implementation.
type localHub struct {
	mu       sync.RWMutex
	channels map[Channel]map[string]Subscriber

	stopped atomic.Bool

	stats struct {
		published     atomic.Int64
		delivered     atomic.Int64
		dropped       atomic.Int64
		remoteIngress atomic.Int64
		remoteEgress  atomic.Int64
	}
}

// NewHub constructs an in-process Hub.
func NewHub() Hub {
	return &localHub{channels: make(map[Channel]map[string]Subscriber)}
}

func (h *localHub) Subscribe(_ context.Context, ch Channel, sub Subscriber) error {
	if h.stopped.Load() {
		return ErrHubStopped
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	subs, ok := h.channels[ch]
	if !ok {
		subs = make(map[string]Subscriber)
		h.channels[ch] = subs
	}
	subs[sub.ID()] = sub
	return nil
}

func (h *localHub) Unsubscribe(ch Channel, sub Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs, ok := h.channels[ch]
	if !ok {
		return
	}
	delete(subs, sub.ID())
	if len(subs) == 0 {
		delete(h.channels, ch)
	}
}

func (h *localHub) Publish(_ context.Context, msg Message) error {
	if h.stopped.Load() {
		return ErrHubStopped
	}
	h.stats.published.Add(1)

	h.mu.RLock()
	subs := h.channels[msg.Channel]
	targets := make([]Subscriber, 0, len(subs))
	for _, s := range subs {
		targets = append(targets, s)
	}
	h.mu.RUnlock()

	for _, s := range targets {
		if err := s.Send(msg); err != nil {
			h.stats.dropped.Add(1)
			continue
		}
		h.stats.delivered.Add(1)
	}
	return nil
}

func (h *localHub) Presence(ch Channel) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	subs, ok := h.channels[ch]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(subs))
	for id := range subs {
		out = append(out, id)
	}
	return out
}

func (h *localHub) Stats() Stats {
	h.mu.RLock()
	channels := len(h.channels)
	subs := 0
	for _, s := range h.channels {
		subs += len(s)
	}
	h.mu.RUnlock()
	return Stats{
		Channels:      channels,
		Subscribers:   subs,
		Published:     h.stats.published.Load(),
		Delivered:     h.stats.delivered.Load(),
		Dropped:       h.stats.dropped.Load(),
		RemoteIngress: h.stats.remoteIngress.Load(),
		RemoteEgress:  h.stats.remoteEgress.Load(),
	}
}

func (h *localHub) Stop() {
	if !h.stopped.CompareAndSwap(false, true) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, subs := range h.channels {
		for _, s := range subs {
			s.Close()
		}
	}
	h.channels = make(map[Channel]map[string]Subscriber)
}

// BufferedSubscriber is a convenience Subscriber backed by a bounded channel.
// On Send to a full buffer it drops the OLDEST queued message and adds the
// new one (so subscribers see the most recent state). The drop is reported
// via the Hub's Stats.Dropped counter.
type BufferedSubscriber struct {
	id      string
	channel Channel
	mu      sync.Mutex
	buf     []Message
	cap     int
	closed  bool
	notify  chan struct{}
}

// NewBufferedSubscriber creates a subscriber with bufSize message capacity.
func NewBufferedSubscriber(id string, ch Channel, bufSize int) *BufferedSubscriber {
	if bufSize < 1 {
		bufSize = 1
	}
	return &BufferedSubscriber{
		id:      id,
		channel: ch,
		cap:     bufSize,
		notify:  make(chan struct{}, 1),
	}
}

func (b *BufferedSubscriber) ID() string       { return b.id }
func (b *BufferedSubscriber) Channel() Channel { return b.channel }

// Send pushes a message into the buffer. Returns an error after Close.
// Drops the oldest message when the buffer is full.
func (b *BufferedSubscriber) Send(m Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errors.New("realtime: subscriber closed")
	}
	if len(b.buf) >= b.cap {
		b.buf = b.buf[1:]
	}
	b.buf = append(b.buf, m)
	select {
	case b.notify <- struct{}{}:
	default:
	}
	return nil
}

// Recv pops the next buffered message; blocks until one is available or the
// subscriber is closed. Returns ok=false when closed and drained.
func (b *BufferedSubscriber) Recv(ctx context.Context) (Message, bool) {
	for {
		b.mu.Lock()
		if len(b.buf) > 0 {
			m := b.buf[0]
			b.buf = b.buf[1:]
			b.mu.Unlock()
			return m, true
		}
		closed := b.closed
		b.mu.Unlock()
		if closed {
			return Message{}, false
		}
		select {
		case <-ctx.Done():
			return Message{}, false
		case <-b.notify:
		}
	}
}

// Close marks the subscriber closed. Buffered messages remain accessible
// via Recv until drained.
func (b *BufferedSubscriber) Close() {
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.notify)
	}
	b.mu.Unlock()
}
