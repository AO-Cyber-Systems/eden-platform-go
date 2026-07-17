// Package email is a transport-agnostic delivery primitive for the Eden
// portfolio. Authoring (templates) is per-product; this package only ships
// the wire format + transports + bounce/complaint webhook decoders.
//
// Donor: aodex-go/internal/email (mailer interface). See TRD 18-04.
package email

import (
	"bytes"
	"context"
	"crypto/tls"
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
	// UseTLS selects implicit TLS (SMTPS): the client opens a TLS connection at
	// connect time instead of starting in plaintext and upgrading via STARTTLS.
	// Required by providers that only accept implicit TLS — e.g. Cloudflare Email
	// (smtp.mx.cloudflare.net:465), which rejects STARTTLS. It is also implied when
	// Port == 465. When false, Send uses the stdlib plaintext+STARTTLS path (e.g.
	// submission port 587).
	UseTLS  bool
	UseAuth bool
	// TLSConfig optionally overrides the tls.Config used for implicit TLS. When
	// nil, a default config verifying the server against the system roots with
	// ServerName == Host (MinVersion TLS 1.2) is used. Primarily a test seam.
	TLSConfig *tls.Config
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

	// Implicit TLS (SMTPS, e.g. :465) and plaintext+STARTTLS (e.g. :587) need
	// different dial paths; the stdlib smtp.SendMail only speaks the latter.
	if s.implicitTLS() {
		if err := s.sendImplicitTLS(ctx, addr, msg.From.Email, to, rendered); err != nil {
			return SendResult{}, fmt.Errorf("email: smtp send (implicit tls): %w", err)
		}
		return SendResult{MessageID: messageID, AcceptedAt: time.Now().UTC()}, nil
	}

	var auth smtp.Auth
	if s.cfg.UseAuth {
		auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	}
	if err := smtp.SendMail(addr, auth, msg.From.Email, to, rendered); err != nil {
		return SendResult{}, fmt.Errorf("email: smtp send: %w", err)
	}
	return SendResult{MessageID: messageID, AcceptedAt: time.Now().UTC()}, nil
}

// implicitTLS reports whether Send should open a TLS connection at connect time
// (SMTPS) rather than upgrading a plaintext connection via STARTTLS. True when
// UseTLS is set or the port is the conventional implicit-TLS submission port 465.
func (s *smtpSender) implicitTLS() bool {
	return s.cfg.UseTLS || s.cfg.Port == 465
}

// sendImplicitTLS delivers rendered over an implicit-TLS (SMTPS) connection.
// net/smtp has no built-in SMTPS dialer, so we dial TLS ourselves, wrap the
// connection with smtp.NewClient, and drive the MAIL/RCPT/DATA sequence. AUTH
// uses a TLS-aware PLAIN mechanism because the stdlib smtp.PlainAuth refuses to
// send credentials when it cannot observe the connection as encrypted — which it
// can't here, since the TLS was established before smtp.NewClient saw the socket.
func (s *smtpSender) sendImplicitTLS(ctx context.Context, addr, from string, to []string, rendered []byte) error {
	tlsCfg := s.cfg.TLSConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}
	}
	conn, err := (&tls.Dialer{Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("tls dial %s: %w", addr, err)
	}
	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer func() { _ = client.Close() }()

	if s.cfg.UseAuth {
		if ok, _ := client.Extension("AUTH"); ok {
			auth := &tlsPlainAuth{username: s.cfg.Username, password: s.cfg.Password, host: s.cfg.Host}
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("auth: %w", err)
			}
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(rendered); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close body: %w", err)
	}
	return client.Quit()
}

// tlsPlainAuth is smtp.PlainAuth without the guard that rejects sending
// credentials over a connection the stdlib can't see as TLS. It is used ONLY on
// connections that are already implicit-TLS (see sendImplicitTLS), so credentials
// never traverse plaintext.
type tlsPlainAuth struct {
	username, password, host string
}

func (a *tlsPlainAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if server.Name != a.host {
		return "", nil, fmt.Errorf("email: unexpected smtp server name %q (want %q)", server.Name, a.host)
	}
	resp := []byte("\x00" + a.username + "\x00" + a.password)
	return "PLAIN", resp, nil
}

func (a *tlsPlainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("email: unexpected server challenge in PLAIN auth")
	}
	return nil, nil
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
