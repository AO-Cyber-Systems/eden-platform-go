package realtime

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestSubscribePublishDeliver(t *testing.T) {
	hub := NewHub()
	defer hub.Stop()
	ctx := context.Background()

	sub := NewBufferedSubscriber("s1", "room", 10)
	if err := hub.Subscribe(ctx, "room", sub); err != nil {
		t.Fatal(err)
	}

	msg := Message{Channel: "room", Type: "chat", Data: json.RawMessage(`"hello"`)}
	if err := hub.Publish(ctx, msg); err != nil {
		t.Fatal(err)
	}

	rxCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	got, ok := sub.Recv(rxCtx)
	if !ok || got.Type != "chat" {
		t.Fatalf("expected chat msg, got %+v ok=%v", got, ok)
	}
}

func TestChannelIsolation(t *testing.T) {
	hub := NewHub()
	defer hub.Stop()
	ctx := context.Background()

	subA := NewBufferedSubscriber("a", "A", 10)
	subB := NewBufferedSubscriber("b", "B", 10)
	_ = hub.Subscribe(ctx, "A", subA)
	_ = hub.Subscribe(ctx, "B", subB)

	_ = hub.Publish(ctx, Message{Channel: "A", Type: "x"})

	rxCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if _, ok := subB.Recv(rxCtx); ok {
		t.Errorf("B should not have received A's message")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	hub := NewHub()
	defer hub.Stop()
	ctx := context.Background()

	sub := NewBufferedSubscriber("s", "ch", 10)
	_ = hub.Subscribe(ctx, "ch", sub)
	hub.Unsubscribe("ch", sub)

	_ = hub.Publish(ctx, Message{Channel: "ch", Type: "x"})

	rxCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if _, ok := sub.Recv(rxCtx); ok {
		t.Errorf("unsubscribed sub should not receive")
	}
}

func TestPresence(t *testing.T) {
	hub := NewHub()
	defer hub.Stop()
	ctx := context.Background()

	subA := NewBufferedSubscriber("alice", "room", 10)
	subB := NewBufferedSubscriber("bob", "room", 10)
	_ = hub.Subscribe(ctx, "room", subA)
	_ = hub.Subscribe(ctx, "room", subB)

	ids := hub.Presence("room")
	if len(ids) != 2 {
		t.Fatalf("expected 2, got %d", len(ids))
	}
	hub.Unsubscribe("room", subA)
	if got := hub.Presence("room"); len(got) != 1 || got[0] != "bob" {
		t.Errorf("expected bob alone, got %v", got)
	}
}

func TestBackpressureDropsOldest(t *testing.T) {
	sub := NewBufferedSubscriber("s", "ch", 2)
	_ = sub.Send(Message{Channel: "ch", Type: "1"})
	_ = sub.Send(Message{Channel: "ch", Type: "2"})
	_ = sub.Send(Message{Channel: "ch", Type: "3"}) // should drop "1"

	rx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	first, _ := sub.Recv(rx)
	second, _ := sub.Recv(rx)
	if first.Type != "2" || second.Type != "3" {
		t.Errorf("expected 2 then 3, got %s then %s", first.Type, second.Type)
	}
}

func TestStatsCount(t *testing.T) {
	hub := NewHub()
	defer hub.Stop()
	ctx := context.Background()

	sub := NewBufferedSubscriber("s", "ch", 10)
	_ = hub.Subscribe(ctx, "ch", sub)

	for i := 0; i < 5; i++ {
		_ = hub.Publish(ctx, Message{Channel: "ch", Type: "x"})
	}
	stats := hub.Stats()
	if stats.Published != 5 || stats.Delivered != 5 {
		t.Errorf("expected 5/5, got %+v", stats)
	}
}

// stubRedis simulates a Redis pub/sub client.
type stubRedis struct {
	mu          sync.Mutex
	publishes   []struct{ ch string; payload []byte }
	subscribers map[string][]chan []byte
}

func newStubRedis() *stubRedis {
	return &stubRedis{subscribers: make(map[string][]chan []byte)}
}

func (s *stubRedis) Publish(_ context.Context, channel string, payload []byte) error {
	s.mu.Lock()
	s.publishes = append(s.publishes, struct{ ch string; payload []byte }{channel, payload})
	subs := append([]chan []byte(nil), s.subscribers[channel]...)
	s.mu.Unlock()
	for _, c := range subs {
		select {
		case c <- payload:
		default:
		}
	}
	return nil
}

func (s *stubRedis) Subscribe(_ context.Context, channel string) (<-chan []byte, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := make(chan []byte, 16)
	s.subscribers[channel] = append(s.subscribers[channel], c)
	cancel := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, existing := range s.subscribers[channel] {
			if existing == c {
				s.subscribers[channel] = append(s.subscribers[channel][:i], s.subscribers[channel][i+1:]...)
				close(c)
				return
			}
		}
	}
	return c, cancel, nil
}

func TestRedisFanoutPublishesToRedis(t *testing.T) {
	rc := newStubRedis()
	hub := NewRedisFanout(NewHub(), rc, "rt:")
	defer hub.Stop()

	ctx := context.Background()
	sub := NewBufferedSubscriber("s", "ch", 10)
	_ = hub.Subscribe(ctx, "ch", sub)

	_ = hub.Publish(ctx, Message{Channel: "ch", Type: "ping"})
	if len(rc.publishes) != 1 || rc.publishes[0].ch != "rt:ch" {
		t.Errorf("expected 1 publish on rt:ch, got %+v", rc.publishes)
	}
}

func TestRedisFanoutCrossInstance(t *testing.T) {
	rc := newStubRedis()
	hubA := NewRedisFanout(NewHub(), rc, "rt:")
	hubB := NewRedisFanout(NewHub(), rc, "rt:")
	defer hubA.Stop()
	defer hubB.Stop()

	ctx := context.Background()
	subB := NewBufferedSubscriber("b", "room", 10)
	_ = hubB.Subscribe(ctx, "room", subB)
	// Wait briefly for stub Redis subscription to be registered.
	time.Sleep(20 * time.Millisecond)

	// Publish from instance A → stub Redis fan-out → B's local hub.
	_ = hubA.Publish(ctx, Message{Channel: "room", Type: "from-a"})

	rxCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	got, ok := subB.Recv(rxCtx)
	if !ok || got.Type != "from-a" {
		t.Errorf("expected B to receive A's publish, got %+v ok=%v", got, ok)
	}
}
