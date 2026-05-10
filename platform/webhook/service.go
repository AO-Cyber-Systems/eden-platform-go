package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Default service limits. Override via Service options.
const (
	DefaultMaxRetries             = 5
	DefaultMaxConsecutiveFailures = 10
	DefaultHTTPTimeout            = 10 * time.Second
	DefaultDeliveryTimeout        = 30 * time.Second
)

// Service handles webhook operations.
type Service struct {
	store                  WebhookStore
	client                 *http.Client
	wg                     sync.WaitGroup
	maxRetries             int
	maxConsecutiveFailures int
	deliveryTimeout        time.Duration
}

// ServiceOption configures a Service.
type ServiceOption func(*Service)

// WithMaxRetries sets the max delivery attempts before status flips to "exhausted".
func WithMaxRetries(n int) ServiceOption {
	return func(s *Service) { s.maxRetries = n }
}

// WithMaxConsecutiveFailures sets the threshold above which a webhook is
// auto-paused (Active=false).
func WithMaxConsecutiveFailures(n int) ServiceOption {
	return func(s *Service) { s.maxConsecutiveFailures = n }
}

// WithHTTPTimeout sets the per-request HTTP timeout (the http.Client.Timeout).
func WithHTTPTimeout(d time.Duration) ServiceOption {
	return func(s *Service) {
		s.client.Timeout = d
	}
}

// WithDeliveryTimeout sets the per-delivery context timeout (overall budget
// for one delivery attempt including I/O).
func WithDeliveryTimeout(d time.Duration) ServiceOption {
	return func(s *Service) { s.deliveryTimeout = d }
}

// WithHTTPClient replaces the underlying HTTP client (for tests, custom
// transports, or telemetry instrumentation).
func WithHTTPClient(c *http.Client) ServiceOption {
	return func(s *Service) { s.client = c }
}

// NewService creates a new webhook service. Defaults match the historical
// behaviour: 5 retries, auto-pause after 10 consecutive failures, 10s HTTP
// timeout, 30s per-delivery context timeout.
func NewService(store WebhookStore, opts ...ServiceOption) *Service {
	s := &Service{
		store: store,
		client: &http.Client{
			Timeout: DefaultHTTPTimeout,
		},
		maxRetries:             DefaultMaxRetries,
		maxConsecutiveFailures: DefaultMaxConsecutiveFailures,
		deliveryTimeout:        DefaultDeliveryTimeout,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Register creates a new webhook subscription.
func (s *Service) Register(ctx context.Context, companyID uuid.UUID, url, secret string, events []string) (Webhook, error) {
	if secret == "" {
		secret = generateSecret()
	}
	return s.store.CreateWebhook(ctx, companyID, url, secret, events)
}

// Trigger fires a webhook event to all matching subscribers.
func (s *Service) Trigger(ctx context.Context, companyID uuid.UUID, eventType, payload string) error {
	webhooks, err := s.store.ListWebhooksByCompany(ctx, companyID)
	if err != nil {
		return fmt.Errorf("list webhooks: %w", err)
	}

	for _, wh := range webhooks {
		if !wh.Active {
			continue
		}
		if !matchesEvent(wh.Events, eventType) {
			continue
		}

		delivery, err := s.store.CreateDelivery(ctx, wh.ID, eventType, payload)
		if err != nil {
			slog.Error("webhook: failed to create delivery", "webhook_id", wh.ID, "error", err)
			continue
		}

		s.wg.Add(1)
		go func(wh Webhook, d WebhookDelivery) {
			defer s.wg.Done()
			s.deliver(ctx, wh, d)
		}(wh, delivery)
	}

	return nil
}

// deliver attempts to deliver a webhook payload.
func (s *Service) deliver(parent context.Context, wh Webhook, delivery WebhookDelivery) {
	ctx, cancel := context.WithTimeout(parent, s.deliveryTimeout)
	defer cancel()

	timestamp := time.Now().Unix()
	signature := sign(wh.Secret, fmt.Sprintf("%d", timestamp), delivery.Payload)
	sigHeader := fmt.Sprintf("t=%d,v1=%s", timestamp, signature)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, strings.NewReader(delivery.Payload))
	if err != nil {
		s.recordFailure(ctx, wh, delivery, 0, err.Error())
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Eden-Signature", sigHeader)
	req.Header.Set("X-Eden-Event", delivery.EventType)
	req.Header.Set("X-Eden-Delivery", delivery.ID.String())

	resp, err := s.client.Do(req)
	if err != nil {
		s.recordFailure(ctx, wh, delivery, 0, err.Error())
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_ = s.store.UpdateDelivery(ctx, delivery.ID, "success", resp.StatusCode, string(body), nil)
		_ = s.store.ResetFailureCount(ctx, wh.ID)
	} else {
		s.recordFailure(ctx, wh, delivery, resp.StatusCode, string(body))
	}
}

func (s *Service) recordFailure(ctx context.Context, wh Webhook, delivery WebhookDelivery, statusCode int, responseBody string) {
	attempt := delivery.Attempts + 1
	status := "failed"

	var nextRetry *time.Time
	if attempt < s.maxRetries {
		retryDelay := time.Duration(math.Pow(2, float64(attempt))) * time.Minute
		t := time.Now().Add(retryDelay)
		nextRetry = &t
	} else {
		status = "exhausted"
	}

	_ = s.store.UpdateDelivery(ctx, delivery.ID, status, statusCode, responseBody, nextRetry)

	failCount, err := s.store.IncrementFailureCount(ctx, wh.ID)
	if err == nil && failCount >= s.maxConsecutiveFailures {
		_ = s.store.UpdateWebhookStatus(ctx, wh.ID, false)
		slog.Warn("webhook auto-paused after consecutive failures",
			"webhook_id", wh.ID, "fail_count", failCount)
	}
}

// Shutdown waits for all in-flight webhook deliveries to complete or the context to expire.
func (s *Service) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sign computes HMAC-SHA256 signature.
func sign(secret, timestamp, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "." + payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature verifies an incoming webhook signature.
//
// The header format is "t=<unix-seconds>,v1=<hex-hmac-sha256>". This function
// validates the signature shape and the HMAC; it does NOT validate the
// timestamp window — receivers should additionally enforce a replay window
// (recommended: 5 minutes). See platform/webhook/README.md for an example.
func VerifySignature(secret, sigHeader, payload string) bool {
	parts := strings.Split(sigHeader, ",")
	if len(parts) != 2 {
		return false
	}

	var timestamp, sig string
	for _, p := range parts {
		if strings.HasPrefix(p, "t=") {
			timestamp = strings.TrimPrefix(p, "t=")
		} else if strings.HasPrefix(p, "v1=") {
			sig = strings.TrimPrefix(p, "v1=")
		}
	}

	expected := sign(secret, timestamp, payload)
	return hmac.Equal([]byte(sig), []byte(expected))
}

func matchesEvent(subscribed []string, eventType string) bool {
	for _, e := range subscribed {
		if e == "*" || e == eventType {
			return true
		}
		if strings.HasSuffix(e, ".*") {
			prefix := strings.TrimSuffix(e, ".*")
			if strings.HasPrefix(eventType, prefix) {
				return true
			}
		}
	}
	return false
}

func generateSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
