package email

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
	"testing"
)

func TestCloudflareSenderSuccess(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	var gotBody cfSendRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{"message_id":"<abc@mail.aocyber.ai>","delivered":["dest@example.com"],"queued":[],"permanent_bounces":[]}}`))
	}))
	defer srv.Close()

	sender := NewCloudflareAPI(CloudflareConfig{
		AccountID: "acct123",
		APIToken:  "cfut_secret",
		BaseURL:   srv.URL,
		HTTPClient: srv.Client(),
	})
	res, err := sender.Send(context.Background(), Message{
		From:     Address{Name: "AO Cyber Systems", Email: "noreply@mail.aocyber.ai"},
		To:       []Address{{Email: "dest@example.com"}},
		Subject:  "hi",
		TextBody: "plain",
		HTMLBody: "<p>rich</p>",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.MessageID != "<abc@mail.aocyber.ai>" {
		t.Errorf("message id: got %q", res.MessageID)
	}
	if gotPath != "/accounts/acct123/email/sending/send" {
		t.Errorf("path: got %q", gotPath)
	}
	if gotAuth != "Bearer cfut_secret" {
		t.Errorf("auth: got %q", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("content-type: got %q", gotCT)
	}
	if gotBody.To != "dest@example.com" || gotBody.Subject != "hi" || gotBody.Text != "plain" || gotBody.HTML != "<p>rich</p>" {
		t.Errorf("body mismatch: %+v", gotBody)
	}
	if !strings.Contains(gotBody.From, "noreply@mail.aocyber.ai") {
		t.Errorf("from missing address: %q", gotBody.From)
	}
}

func TestCloudflareSenderAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}],"result":null}`))
	}))
	defer srv.Close()

	sender := NewCloudflareAPI(CloudflareConfig{AccountID: "a", APIToken: "bad", BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := sender.Send(context.Background(), Message{
		From: Address{Email: "noreply@mail.aocyber.ai"},
		To:   []Address{{Email: "dest@example.com"}},
	})
	if err == nil {
		t.Fatal("expected error on non-success response")
	}
	if !strings.Contains(err.Error(), "10000") || !strings.Contains(err.Error(), "Authentication error") {
		t.Errorf("error should surface the API error: %v", err)
	}
}

func TestCloudflareSenderRejectsInvalid(t *testing.T) {
	sender := NewCloudflareAPI(CloudflareConfig{AccountID: "a", APIToken: "t"})
	_, err := sender.Send(context.Background(), Message{Subject: "no from no to"})
	if err == nil {
		t.Fatal("expected ErrInvalidMessage for message with no From/To")
	}
}

func TestCloudflareSenderMisconfigured(t *testing.T) {
	sender := NewCloudflareAPI(CloudflareConfig{APIToken: "t"}) // no account id
	_, err := sender.Send(context.Background(), Message{
		From: Address{Email: "a@b.com"},
		To:   []Address{{Email: "c@d.com"}},
	})
	if err == nil {
		t.Fatal("expected error when account id is empty")
	}
}

// --- Retry on throttle (aodex#616) -----------------------------------------
//
// Cloudflare documents 429 / 10004 (email.sending.error.throttled) as
// RETRYABLE. Before this behaviour existed a single 429 surfaced as a permanent
// failure and the message was simply dropped -- which is how AODex signups
// stopped receiving activation email while the shared, account-wide Email
// Sending quota was exhausted.

func retryTestMessage() Message {
	return Message{
		From:     Address{Name: "AO Cyber Systems", Email: "noreply@mail.aocyber.ai"},
		To:       []Address{{Email: "dest@example.com"}},
		Subject:  "Activate your AO ID account",
		TextBody: "hello",
	}
}

const cfThrottled = `{"success":false,"errors":[{"code":10004,"message":"email.sending.error.throttled"}],"result":null}`
const cfOK = `{"success":true,"errors":[],"result":{"message_id":"<ok@mail.aocyber.ai>"}}`

func TestCloudflareRetriesThrottleThenSucceeds(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(cfThrottled))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cfOK))
	}))
	defer srv.Close()

	sender := NewCloudflareAPI(CloudflareConfig{
		AccountID: "a", APIToken: "t", BaseURL: srv.URL, HTTPClient: srv.Client(),
		RetryBaseDelay: time.Millisecond, // keep the test fast
	})
	res, err := sender.Send(context.Background(), retryTestMessage())
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
	if res.MessageID != "<ok@mail.aocyber.ai>" {
		t.Errorf("unexpected message id %q", res.MessageID)
	}
}

func TestCloudflareGivesUpAfterMaxAttempts(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(cfThrottled))
	}))
	defer srv.Close()

	sender := NewCloudflareAPI(CloudflareConfig{
		AccountID: "a", APIToken: "t", BaseURL: srv.URL, HTTPClient: srv.Client(),
		MaxAttempts: 3, RetryBaseDelay: time.Millisecond,
	})
	_, err := sender.Send(context.Background(), retryTestMessage())
	if err == nil {
		t.Fatal("expected failure when throttled on every attempt")
	}
	if calls != 3 {
		t.Errorf("expected exactly 3 attempts, got %d", calls)
	}
	// The original error must survive so existing caller log lines still match.
	if !strings.Contains(err.Error(), "cloudflare send failed") || !strings.Contains(err.Error(), "10004") {
		t.Errorf("error lost its original cause: %v", err)
	}
	if !strings.Contains(err.Error(), "gave up after 3 attempts") {
		t.Errorf("error should say it exhausted retries: %v", err)
	}
}

func TestCloudflareDoesNotRetryPermanentFault(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10102,"message":"authentication.forbidden"}],"result":null}`))
	}))
	defer srv.Close()

	sender := NewCloudflareAPI(CloudflareConfig{
		AccountID: "a", APIToken: "bad", BaseURL: srv.URL, HTTPClient: srv.Client(),
		RetryBaseDelay: time.Millisecond,
	})
	if _, err := sender.Send(context.Background(), retryTestMessage()); err == nil {
		t.Fatal("expected failure")
	}
	// Retrying a bad token only burns quota that is already scarce.
	if calls != 1 {
		t.Errorf("permanent fault must not be retried; got %d attempts", calls)
	}
}

func TestCloudflareHonorsRetryAfter(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(cfThrottled))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cfOK))
	}))
	defer srv.Close()

	sender := NewCloudflareAPI(CloudflareConfig{
		AccountID: "a", APIToken: "t", BaseURL: srv.URL, HTTPClient: srv.Client(),
		RetryBaseDelay: time.Millisecond, // far shorter than Retry-After
	})
	start := time.Now()
	if _, err := sender.Send(context.Background(), retryTestMessage()); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	// Server's 1s hint must win over the 1ms computed backoff.
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("Retry-After ignored: retried after %v, expected >= 1s", elapsed)
	}
}

func TestCloudflareRetryAbandonedOnContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(cfThrottled))
	}))
	defer srv.Close()

	sender := NewCloudflareAPI(CloudflareConfig{
		AccountID: "a", APIToken: "t", BaseURL: srv.URL, HTTPClient: srv.Client(),
		MaxAttempts: 50, RetryBaseDelay: 200 * time.Millisecond,
	})
	// A signup blocking indefinitely on email retries is worse than a late email.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := sender.Send(ctx, retryTestMessage()); err == nil {
		t.Fatal("expected failure once the context expired")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("did not abandon retries on context cancel: took %v", elapsed)
	}
}
