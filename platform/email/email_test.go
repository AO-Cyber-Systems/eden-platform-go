package email

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"testing"
)

func TestRecorderRoundTrip(t *testing.T) {
	r := NewRecorder()
	msg := Message{
		From:     Address{Name: "Sys", Email: "noreply@example.com"},
		To:       []Address{{Email: "user@example.com"}},
		Subject:  "Hi",
		TextBody: "Hello there",
	}
	res, err := r.Send(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	if res.MessageID == "" {
		t.Errorf("expected non-empty message id")
	}
	got := r.Sent()
	if len(got) != 1 || got[0].Subject != "Hi" {
		t.Errorf("expected 1 sent, got %+v", got)
	}
}

func TestRecorderRejectsInvalid(t *testing.T) {
	r := NewRecorder()
	_, err := r.Send(context.Background(), Message{Subject: "no from no to"})
	if !errors.Is(err, ErrInvalidMessage) {
		t.Errorf("expected ErrInvalidMessage, got %v", err)
	}
}

func TestRenderSinglePartText(t *testing.T) {
	msg := Message{
		From:     Address{Email: "a@example.com"},
		To:       []Address{{Email: "b@example.com"}},
		Subject:  "hello",
		TextBody: "world",
	}
	out, mid, err := Render(msg)
	if err != nil {
		t.Fatal(err)
	}
	if mid == "" {
		t.Errorf("expected non-empty message id")
	}
	parsed, err := mail.ReadMessage(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(parsed.Header.Get("Content-Type"), "text/plain") {
		t.Errorf("expected text/plain content-type: %q", parsed.Header.Get("Content-Type"))
	}
}

func TestRenderHTMLOnly(t *testing.T) {
	msg := Message{
		From:     Address{Email: "a@example.com"},
		To:       []Address{{Email: "b@example.com"}},
		Subject:  "html",
		HTMLBody: "<p>hi</p>",
	}
	out, _, err := Render(msg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "Content-Type: text/html") {
		t.Errorf("expected text/html in output")
	}
	if !strings.Contains(string(out), "<p>hi</p>") {
		t.Errorf("expected html body")
	}
}

func TestRenderMultipartAlternative(t *testing.T) {
	msg := Message{
		From:     Address{Email: "a@example.com"},
		To:       []Address{{Email: "b@example.com"}},
		Subject:  "alt",
		TextBody: "plain",
		HTMLBody: "<p>html</p>",
	}
	out, _, err := Render(msg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "multipart/alternative") {
		t.Errorf("expected multipart/alternative, got: %s", string(out)[:200])
	}
	if !strings.Contains(string(out), "plain") || !strings.Contains(string(out), "<p>html</p>") {
		t.Errorf("expected both bodies in output")
	}
}

func TestRenderWithAttachment(t *testing.T) {
	msg := Message{
		From:     Address{Email: "a@example.com"},
		To:       []Address{{Email: "b@example.com"}},
		Subject:  "att",
		TextBody: "body",
		Attachments: []Attachment{
			{Filename: "hello.txt", ContentType: "text/plain", Data: []byte("ATTACHED")},
		},
	}
	out, _, err := Render(msg)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "multipart/mixed") {
		t.Errorf("expected multipart/mixed")
	}
	if !strings.Contains(s, `filename="hello.txt"`) {
		t.Errorf("expected filename header")
	}
	// base64 encoded "ATTACHED" is "QVRUQUNIRUQ="
	if !strings.Contains(s, "QVRUQUNIRUQ=") {
		t.Errorf("expected base64 attachment payload")
	}
}

func TestParseSESWebhookBounce(t *testing.T) {
	payload := []byte(`{
		"notificationType": "Bounce",
		"mail": {"messageId": "abc-123", "timestamp": "2025-01-01T00:00:00Z"},
		"bounce": {"bouncedRecipients": [{"emailAddress": "bounce@example.com"}]}
	}`)
	ev, err := ParseSESWebhook(payload)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "bounce" || ev.Email != "bounce@example.com" || ev.MessageID != "abc-123" {
		t.Errorf("unexpected: %+v", ev)
	}
}

func TestParseSESWebhookComplaint(t *testing.T) {
	payload := []byte(`{
		"notificationType": "Complaint",
		"mail": {"messageId": "xyz", "timestamp": "2025-01-01T00:00:00Z"},
		"complaint": {"complainedRecipients": [{"emailAddress": "spam@example.com"}]}
	}`)
	ev, err := ParseSESWebhook(payload)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "complaint" || ev.Email != "spam@example.com" {
		t.Errorf("unexpected: %+v", ev)
	}
}

func TestParseSESWebhookSNSWrapped(t *testing.T) {
	inner := `{"notificationType":"Bounce","mail":{"messageId":"m"},"bounce":{"bouncedRecipients":[{"emailAddress":"x@y"}]}}`
	wrapped := []byte(`{"Type":"Notification","Message":` + asJSONString(inner) + `}`)
	ev, err := ParseSESWebhook(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "bounce" || ev.Email != "x@y" {
		t.Errorf("unwrap failed: %+v", ev)
	}
}

func TestParseSESWebhookMalformed(t *testing.T) {
	if _, err := ParseSESWebhook([]byte("not json")); err == nil {
		t.Errorf("expected error on malformed input")
	}
	if _, err := ParseSESWebhook(nil); !errors.Is(err, ErrInvalidMessage) {
		t.Errorf("expected ErrInvalidMessage on empty payload")
	}
}

func asJSONString(s string) string {
	// Minimal escape for embedding in JSON
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func TestAddressString(t *testing.T) {
	if got := (Address{Email: "a@b"}).String(); got != "a@b" {
		t.Errorf("got %q", got)
	}
	if got := (Address{Name: "A", Email: "a@b"}).String(); got != "A <a@b>" {
		t.Errorf("got %q", got)
	}
}
