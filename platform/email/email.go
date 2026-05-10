// Package email is a transport-agnostic delivery primitive for the Eden
// portfolio. Authoring (templates) is per-product; this package only ships
// the wire format + transports + bounce/complaint webhook decoders.
//
// Donor: aodex-go/internal/email (mailer interface). See TRD 18-04.
package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Address is an addressable email recipient/sender.
type Address struct {
	Name  string
	Email string
}

// String formats Address as "Name <email>" (or just "email" when Name is empty).
func (a Address) String() string {
	if a.Name == "" {
		return a.Email
	}
	return fmt.Sprintf("%s <%s>", a.Name, a.Email)
}

// Attachment is a file attached to a Message.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Message is the wire format for one email.
type Message struct {
	From        Address
	To          []Address
	Cc          []Address
	Bcc         []Address
	Subject     string
	HTMLBody    string
	TextBody    string
	ReplyTo     []Address
	Headers     map[string]string
	Attachments []Attachment
}

// SendResult is returned on successful send.
type SendResult struct {
	MessageID  string
	AcceptedAt time.Time
}

// Sender delivers a Message. Implementations: SMTP, Recorder (test),
// SES (deferred — interface is here for forward compatibility).
type Sender interface {
	Send(ctx context.Context, msg Message) (SendResult, error)
}

// ErrInvalidMessage is returned when required fields are missing.
var ErrInvalidMessage = errors.New("email: invalid message (need From and at least one To)")

// SMTPConfig configures the SMTP transport.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	UseTLS   bool
	UseAuth  bool
}

// smtpSender is the production SMTP transport.
type smtpSender struct {
	cfg SMTPConfig
}

// NewSMTP constructs an SMTP-backed Sender.
func NewSMTP(cfg SMTPConfig) Sender { return &smtpSender{cfg: cfg} }

func (s *smtpSender) Send(ctx context.Context, msg Message) (SendResult, error) {
	if err := validate(msg); err != nil {
		return SendResult{}, err
	}
	rendered, messageID, err := Render(msg)
	if err != nil {
		return SendResult{}, err
	}

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	var auth smtp.Auth
	if s.cfg.UseAuth {
		auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	}

	to := make([]string, 0, len(msg.To)+len(msg.Cc)+len(msg.Bcc))
	for _, a := range msg.To {
		to = append(to, a.Email)
	}
	for _, a := range msg.Cc {
		to = append(to, a.Email)
	}
	for _, a := range msg.Bcc {
		to = append(to, a.Email)
	}

	if err := smtp.SendMail(addr, auth, msg.From.Email, to, rendered); err != nil {
		return SendResult{}, fmt.Errorf("email: smtp send: %w", err)
	}
	return SendResult{MessageID: messageID, AcceptedAt: time.Now().UTC()}, nil
}

// Recorder is a test Sender that captures every Send and never networks.
type Recorder struct {
	mu   sync.Mutex
	sent []Message
}

// NewRecorder constructs a Recorder.
func NewRecorder() *Recorder { return &Recorder{} }

// Send captures msg.
func (r *Recorder) Send(_ context.Context, msg Message) (SendResult, error) {
	if err := validate(msg); err != nil {
		return SendResult{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, msg)
	return SendResult{MessageID: uuid.New().String() + "@test", AcceptedAt: time.Now().UTC()}, nil
}

// Sent returns a snapshot of recorded messages.
func (r *Recorder) Sent() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Message, len(r.sent))
	copy(out, r.sent)
	return out
}

// Reset clears the recorded messages.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = nil
}

func validate(m Message) error {
	if m.From.Email == "" || len(m.To) == 0 {
		return ErrInvalidMessage
	}
	return nil
}

// Render serializes a Message into RFC 5322 wire format. Returns the rendered
// bytes and the assigned Message-ID. Useful for tests and SES `RawEmail` sends.
func Render(m Message) ([]byte, string, error) {
	if err := validate(m); err != nil {
		return nil, "", err
	}
	messageID := fmt.Sprintf("<%s@%s>", uuid.New().String(), domainOf(m.From.Email))

	var buf bytes.Buffer
	headers := textproto.MIMEHeader{}
	headers.Set("From", m.From.String())
	headers.Set("To", joinAddrs(m.To))
	if len(m.Cc) > 0 {
		headers.Set("Cc", joinAddrs(m.Cc))
	}
	if len(m.ReplyTo) > 0 {
		headers.Set("Reply-To", joinAddrs(m.ReplyTo))
	}
	headers.Set("Subject", mime.QEncoding.Encode("UTF-8", m.Subject))
	headers.Set("Message-ID", messageID)
	headers.Set("Date", time.Now().UTC().Format(time.RFC1123Z))
	headers.Set("MIME-Version", "1.0")
	for k, v := range m.Headers {
		headers.Set(k, v)
	}

	hasAttachments := len(m.Attachments) > 0
	hasAlt := m.HTMLBody != "" && m.TextBody != ""

	if !hasAttachments && !hasAlt {
		// Single-part body
		if m.HTMLBody != "" {
			headers.Set("Content-Type", "text/html; charset=UTF-8")
			writeHeaders(&buf, headers)
			buf.WriteString(m.HTMLBody)
		} else {
			headers.Set("Content-Type", "text/plain; charset=UTF-8")
			writeHeaders(&buf, headers)
			buf.WriteString(m.TextBody)
		}
		return buf.Bytes(), messageID, nil
	}

	// Multipart
	boundary := "boundary_" + uuid.New().String()
	if hasAttachments {
		headers.Set("Content-Type", `multipart/mixed; boundary="`+boundary+`"`)
	} else {
		headers.Set("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	}
	writeHeaders(&buf, headers)

	mw := multipart.NewWriter(&buf)
	if err := mw.SetBoundary(boundary); err != nil {
		return nil, "", fmt.Errorf("email: set boundary: %w", err)
	}

	if hasAlt {
		altBoundary := "alt_" + uuid.New().String()
		altHeaders := textproto.MIMEHeader{}
		altHeaders.Set("Content-Type", `multipart/alternative; boundary="`+altBoundary+`"`)
		altPart, err := mw.CreatePart(altHeaders)
		if err != nil {
			return nil, "", err
		}
		altMW := multipart.NewWriter(altPart)
		if err := altMW.SetBoundary(altBoundary); err != nil {
			return nil, "", err
		}
		if err := writeBodyPart(altMW, "text/plain", m.TextBody); err != nil {
			return nil, "", err
		}
		if err := writeBodyPart(altMW, "text/html", m.HTMLBody); err != nil {
			return nil, "", err
		}
		if err := altMW.Close(); err != nil {
			return nil, "", err
		}
	} else {
		// Just HTML or just text + attachments — single body part.
		if m.HTMLBody != "" {
			if err := writeBodyPart(mw, "text/html", m.HTMLBody); err != nil {
				return nil, "", err
			}
		}
		if m.TextBody != "" && m.HTMLBody == "" {
			if err := writeBodyPart(mw, "text/plain", m.TextBody); err != nil {
				return nil, "", err
			}
		}
	}

	for _, att := range m.Attachments {
		ct := att.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		attHeaders := textproto.MIMEHeader{}
		attHeaders.Set("Content-Type", fmt.Sprintf(`%s; name="%s"`, ct, att.Filename))
		attHeaders.Set("Content-Transfer-Encoding", "base64")
		attHeaders.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, att.Filename))

		part, err := mw.CreatePart(attHeaders)
		if err != nil {
			return nil, "", err
		}
		enc := base64.NewEncoder(base64.StdEncoding, part)
		if _, err := io.Copy(enc, bytes.NewReader(att.Data)); err != nil {
			return nil, "", err
		}
		if err := enc.Close(); err != nil {
			return nil, "", err
		}
	}

	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), messageID, nil
}

func writeBodyPart(mw *multipart.Writer, contentType, body string) error {
	headers := textproto.MIMEHeader{}
	headers.Set("Content-Type", contentType+"; charset=UTF-8")
	part, err := mw.CreatePart(headers)
	if err != nil {
		return err
	}
	_, err = io.WriteString(part, body)
	return err
}

func writeHeaders(w *bytes.Buffer, headers textproto.MIMEHeader) {
	for k, vs := range headers {
		for _, v := range vs {
			fmt.Fprintf(w, "%s: %s\r\n", k, v)
		}
	}
	w.WriteString("\r\n")
}

func joinAddrs(addrs []Address) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		parts = append(parts, a.String())
	}
	return strings.Join(parts, ", ")
}

func domainOf(emailAddr string) string {
	if i := strings.LastIndex(emailAddr, "@"); i > 0 {
		return emailAddr[i+1:]
	}
	return "localhost"
}

// WebhookEvent is a normalized bounce/complaint event from a delivery provider.
type WebhookEvent struct {
	Type      string    // "bounce", "complaint", "delivery"
	MessageID string    // provider's message id
	Email     string    // affected recipient
	Timestamp time.Time
	Raw       []byte // original payload (kept for debugging)
}

// ParseSESWebhook decodes an AWS SES SNS notification payload into a
// WebhookEvent. Returns ErrInvalidMessage if the payload is malformed.
//
// Expected payload shape (SNS-wrapped or raw):
//
//	{
//	  "notificationType": "Bounce" | "Complaint" | "Delivery",
//	  "mail": {"messageId": "...", "timestamp": "..."},
//	  "bounce": {"bouncedRecipients": [{"emailAddress": "..."}]},
//	  "complaint": {"complainedRecipients": [{"emailAddress": "..."}]}
//	}
func ParseSESWebhook(payload []byte) (WebhookEvent, error) {
	if len(payload) == 0 {
		return WebhookEvent{}, ErrInvalidMessage
	}

	// Try unwrapping SNS first
	var snsWrap struct {
		Type    string `json:"Type"`
		Message string `json:"Message"`
	}
	if err := json.Unmarshal(payload, &snsWrap); err == nil && snsWrap.Type == "Notification" && snsWrap.Message != "" {
		return ParseSESWebhook([]byte(snsWrap.Message))
	}

	var raw struct {
		NotificationType string `json:"notificationType"`
		Mail             struct {
			MessageID string    `json:"messageId"`
			Timestamp time.Time `json:"timestamp"`
		} `json:"mail"`
		Bounce *struct {
			BouncedRecipients []struct {
				EmailAddress string `json:"emailAddress"`
			} `json:"bouncedRecipients"`
		} `json:"bounce,omitempty"`
		Complaint *struct {
			ComplainedRecipients []struct {
				EmailAddress string `json:"emailAddress"`
			} `json:"complainedRecipients"`
		} `json:"complaint,omitempty"`
		Delivery *struct {
			Recipients []string `json:"recipients"`
		} `json:"delivery,omitempty"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return WebhookEvent{}, fmt.Errorf("email: parse SES webhook: %w", err)
	}

	ev := WebhookEvent{
		MessageID: raw.Mail.MessageID,
		Timestamp: raw.Mail.Timestamp,
		Raw:       payload,
	}
	switch strings.ToLower(raw.NotificationType) {
	case "bounce":
		ev.Type = "bounce"
		if raw.Bounce != nil && len(raw.Bounce.BouncedRecipients) > 0 {
			ev.Email = raw.Bounce.BouncedRecipients[0].EmailAddress
		}
	case "complaint":
		ev.Type = "complaint"
		if raw.Complaint != nil && len(raw.Complaint.ComplainedRecipients) > 0 {
			ev.Email = raw.Complaint.ComplainedRecipients[0].EmailAddress
		}
	case "delivery":
		ev.Type = "delivery"
		if raw.Delivery != nil && len(raw.Delivery.Recipients) > 0 {
			ev.Email = raw.Delivery.Recipients[0]
		}
	default:
		return WebhookEvent{}, fmt.Errorf("email: unknown notification type %q", raw.NotificationType)
	}
	return ev, nil
}
