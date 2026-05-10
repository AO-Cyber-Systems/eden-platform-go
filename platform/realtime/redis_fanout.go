package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

// RedisClient is the minimal Redis pub/sub surface used by RedisFanout.
// Adapter pattern: don't import go-redis at the package boundary.
type RedisClient interface {
	Publish(ctx context.Context, channel string, payload []byte) error
	// Subscribe returns a receive-only channel of payloads delivered on the
	// matched Redis channel pattern, plus a cancel function. The cancel
	// function MUST be safe to call multiple times.
	Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error)
}

// fanoutHub wraps a local Hub, mirroring Publish() out to Redis pub/sub
// and re-broadcasting incoming Redis messages back into the local Hub.
//
// Each subscribe to a NEW channel triggers a Redis subscription on
// `<prefix><channel>`. Redis subscriptions are cleaned up when the channel
// has no local subscribers (or on Stop).
type fanoutHub struct {
	local  Hub
	redis  RedisClient
	prefix string

	mu        sync.Mutex
	cancels   map[Channel]func()
	subCount  map[Channel]int
	stopCtx   context.Context
	stopFn    context.CancelFunc
}

// NewRedisFanout wraps local with a Redis pub/sub fanout. prefix is
// prepended to every Redis channel (e.g. "myapp:rt:").
func NewRedisFanout(local Hub, redis RedisClient, prefix string) Hub {
	if redis == nil {
		return local
	}
	if prefix == "" {
		prefix = "platform:rt:"
	}
	stopCtx, stopFn := context.WithCancel(context.Background())
	return &fanoutHub{
		local:    local,
		redis:    redis,
		prefix:   prefix,
		cancels:  make(map[Channel]func()),
		subCount: make(map[Channel]int),
		stopCtx:  stopCtx,
		stopFn:   stopFn,
	}
}

func (f *fanoutHub) Subscribe(ctx context.Context, ch Channel, sub Subscriber) error {
	if err := f.local.Subscribe(ctx, ch, sub); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	f.subCount[ch]++
	if _, ok := f.cancels[ch]; ok {
		return nil // already subscribed to Redis
	}

	rxCh, cancel, err := f.redis.Subscribe(f.stopCtx, f.prefix+string(ch))
	if err != nil {
		// Local subscribe succeeded; we just won't receive cross-instance fanout.
		slog.Warn("[realtime] redis subscribe failed; cross-instance fanout disabled for channel",
			"channel", ch, "error", err)
		return nil
	}
	f.cancels[ch] = cancel
	go f.consumeRedis(rxCh, ch)
	return nil
}

func (f *fanoutHub) Unsubscribe(ch Channel, sub Subscriber) {
	f.local.Unsubscribe(ch, sub)
	f.mu.Lock()
	defer f.mu.Unlock()

	f.subCount[ch]--
	if f.subCount[ch] <= 0 {
		delete(f.subCount, ch)
		if cancel, ok := f.cancels[ch]; ok {
			cancel()
			delete(f.cancels, ch)
		}
	}
}

func (f *fanoutHub) Publish(ctx context.Context, msg Message) error {
	if err := f.local.Publish(ctx, msg); err != nil {
		return err
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("realtime: marshal message: %w", err)
	}
	if err := f.redis.Publish(ctx, f.prefix+string(msg.Channel), payload); err != nil {
		slog.Warn("[realtime] redis publish failed; only local subscribers received",
			"channel", msg.Channel, "error", err)
		// Best-effort — don't fail the local Publish.
	}
	return nil
}

func (f *fanoutHub) Presence(ch Channel) []string { return f.local.Presence(ch) }

func (f *fanoutHub) Stats() Stats { return f.local.Stats() }

func (f *fanoutHub) Stop() {
	f.stopFn()
	f.mu.Lock()
	for _, c := range f.cancels {
		c()
	}
	f.cancels = make(map[Channel]func())
	f.subCount = make(map[Channel]int)
	f.mu.Unlock()
	f.local.Stop()
}

// consumeRedis is the goroutine that re-broadcasts Redis messages locally.
// We deliberately do NOT re-publish out to Redis (that would loop).
func (f *fanoutHub) consumeRedis(rx <-chan []byte, ch Channel) {
	for payload := range rx {
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			slog.Warn("[realtime] decode redis message failed", "channel", ch, "error", err)
			continue
		}
		// Bypass our Publish (which would re-emit to Redis); use the local hub directly.
		if err := f.local.Publish(f.stopCtx, msg); err != nil {
			slog.Debug("[realtime] local publish from redis failed", "error", err)
		}
		if h, ok := f.local.(*localHub); ok {
			h.stats.remoteIngress.Add(1)
		}
	}
}
