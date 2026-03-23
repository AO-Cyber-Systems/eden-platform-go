package bridge

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Adapter transforms platform events.
type Adapter interface {
	EventTypes() []string
	Transform(subject string, envelope EventEnvelope) (*TransformedEvent, error)
	ActionTypes() []ActionSchema
	SupportsAction(actionType string) bool
}

// ActionSchema describes an action available on a platform event.
type ActionSchema struct {
	Type          string `json:"type"`
	Label         string `json:"label"`
	RequiresInput bool   `json:"requires_input"`
	InputHint     string `json:"input_hint,omitempty"`
	Destructive   bool   `json:"destructive"`
}

// EventEnvelope is the wire format for platform events.
type EventEnvelope struct {
	AppName    string          `json:"app_name"`
	EventID    string          `json:"event_id"`
	EventType  string          `json:"event_type"`
	CompanyID  string          `json:"company_id"`
	UserID     string          `json:"user_id,omitempty"`
	Timestamp  time.Time       `json:"timestamp"`
	Data       json.RawMessage `json:"data"`
}

// TransformedEvent is the output of adapter transformation.
type TransformedEvent struct {
	EventType string          `json:"event_type"`
	SourceID  string          `json:"source_id"`
	CompanyID string          `json:"company_id"`
	Data      json.RawMessage `json:"data"`
}

// AdapterRegistry maps subject prefixes to adapters.
type AdapterRegistry struct {
	adapters   map[string]Adapter
	eventTypes map[string]Adapter
}

// NewAdapterRegistry creates an empty adapter registry.
func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{
		adapters:   make(map[string]Adapter),
		eventTypes: make(map[string]Adapter),
	}
}

// Register associates a subject prefix with an adapter.
func (r *AdapterRegistry) Register(prefix string, adapter Adapter) {
	r.adapters[prefix] = adapter
	for _, et := range adapter.EventTypes() {
		r.eventTypes[et] = adapter
	}
}

// FindAdapter returns the adapter matching the given subject by longest prefix.
func (r *AdapterRegistry) FindAdapter(subject string) (Adapter, bool) {
	var best Adapter
	bestLen := 0
	for prefix, adapter := range r.adapters {
		if strings.HasPrefix(subject, prefix) && len(prefix) > bestLen {
			best = adapter
			bestLen = len(prefix)
		}
	}
	return best, best != nil
}

// EventHandler processes transformed events (e.g., publishes to timeline).
type EventHandler func(ctx context.Context, subject string, event TransformedEvent) error

// BridgeService consumes events from NATS and dispatches to adapters.
type BridgeService struct {
	js           jetstream.JetStream
	conn         *nats.Conn
	registry     *AdapterRegistry
	handler      EventHandler
	streamName   string
	subjectRoot  string
	consumerName string
	wg           sync.WaitGroup
	cancel       context.CancelFunc
}

// BridgeConfig holds configuration for the bridge service.
type BridgeConfig struct {
	NatsURL      string
	StreamName   string
	SubjectRoot  string
	ConsumerName string
}

// NewBridgeService creates a new bridge service.
func NewBridgeService(cfg BridgeConfig, registry *AdapterRegistry, handler EventHandler) *BridgeService {
	conn, err := nats.Connect(cfg.NatsURL)
	if err != nil {
		slog.Warn("failed to connect to NATS for bridge", "url", cfg.NatsURL, "error", err)
		return &BridgeService{registry: registry, handler: handler}
	}

	js, err := jetstream.New(conn)
	if err != nil {
		slog.Warn("failed to create JetStream context", "error", err)
		conn.Close()
		return &BridgeService{registry: registry, handler: handler}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	streamName := cfg.StreamName
	if streamName == "" {
		streamName = "EDEN_PLATFORM"
	}
	subjectRoot := cfg.SubjectRoot
	if subjectRoot == "" {
		subjectRoot = "eden.platform.>"
	}

	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{subjectRoot},
		Storage:  jetstream.FileStorage,
	})
	if err != nil {
		slog.Warn("failed to create stream", "name", streamName, "error", err)
		conn.Close()
		return &BridgeService{registry: registry, handler: handler}
	}

	return &BridgeService{
		js:           js,
		conn:         conn,
		registry:     registry,
		handler:      handler,
		streamName:   streamName,
		subjectRoot:  subjectRoot,
		consumerName: cfg.ConsumerName,
	}
}

// JetStream returns the JetStream context (may be nil).
func (b *BridgeService) JetStream() jetstream.JetStream {
	return b.js
}

// Start begins consuming events.
func (b *BridgeService) Start(ctx context.Context) error {
	if b.js == nil {
		slog.Warn("bridge service not started: NATS not connected")
		return nil
	}

	consumerName := b.consumerName
	if consumerName == "" {
		consumerName = "platform-bridge"
	}

	cons, err := b.js.CreateOrUpdateConsumer(ctx, b.streamName, jetstream.ConsumerConfig{
		Durable:       consumerName,
		FilterSubject: b.subjectRoot,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel

	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			msg, err := cons.Next(jetstream.FetchMaxWait(5 * time.Second))
			if err != nil {
				continue
			}
			b.processMessage(ctx, msg)
		}
	}()

	return nil
}

func (b *BridgeService) processMessage(ctx context.Context, msg jetstream.Msg) {
	subject := msg.Subject()

	adapter, found := b.registry.FindAdapter(subject)
	if !found {
		slog.Warn("no adapter for event", "subject", subject)
		_ = msg.Nak()
		return
	}

	var envelope EventEnvelope
	if err := json.Unmarshal(msg.Data(), &envelope); err != nil {
		slog.Error("unmarshal event failed", "subject", subject, "error", err)
		_ = msg.Nak()
		return
	}

	event, err := adapter.Transform(subject, envelope)
	if err != nil {
		slog.Error("adapter transform failed", "subject", subject, "error", err)
		_ = msg.Nak()
		return
	}

	if event == nil {
		_ = msg.Ack()
		return
	}

	if b.handler != nil {
		if err := b.handler(ctx, subject, *event); err != nil {
			slog.Error("event handler failed", "subject", subject, "error", err)
			_ = msg.Nak()
			return
		}
	}

	_ = msg.Ack()
}

// Stop cancels the consumer goroutine, waits for it to finish, and closes the NATS connection.
func (b *BridgeService) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	b.wg.Wait()
	if b.conn != nil {
		b.conn.Close()
	}
}

// Close shuts down the NATS connection. Deprecated: use Stop() for graceful shutdown.
func (b *BridgeService) Close() {
	b.Stop()
}
