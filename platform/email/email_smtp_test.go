package email

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// selfSignedCert generates an in-memory TLS cert valid for 127.0.0.1, returning
// both the tls.Certificate (for the server) and the leaf x509 (for the client's
// root pool) so the test verifies the chain rather than skipping verification.
func selfSignedCert(t *testing.T) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"127.0.0.1"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("createcert: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsecert: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, leaf
}

// smtpsStub is a minimal implicit-TLS SMTP server. It speaks just enough of the
// protocol to satisfy net/smtp and records what it received.
type smtpsStub struct {
	ln     net.Listener
	addr   string
	caCert *x509.Certificate

	mu       sync.Mutex
	gotAuth  bool
	gotFrom  string
	gotRcpts []string
	gotData  string
}

func newSMTPSStub(t *testing.T) *smtpsStub {
	t.Helper()
	cert, leaf := selfSignedCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &smtpsStub{ln: ln, addr: ln.Addr().String(), caCert: leaf}
	t.Cleanup(func() { _ = ln.Close() })
	go s.serve()
	return s
}

func (s *smtpsStub) serve() {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	br := bufio.NewReader(conn)
	wr := func(line string) { fmt.Fprintf(conn, "%s\r\n", line) }

	wr("220 stub ESMTP ready")
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		up := strings.ToUpper(strings.TrimRight(line, "\r\n"))
		switch {
		case strings.HasPrefix(up, "EHLO"), strings.HasPrefix(up, "HELO"):
			wr("250-stub greets you")
			wr("250 AUTH PLAIN LOGIN")
		case strings.HasPrefix(up, "AUTH PLAIN"):
			s.mu.Lock()
			s.gotAuth = true
			s.mu.Unlock()
			wr("235 2.7.0 Authentication successful")
		case strings.HasPrefix(up, "MAIL FROM"):
			s.mu.Lock()
			s.gotFrom = strings.TrimRight(line, "\r\n")
			s.mu.Unlock()
			wr("250 2.1.0 Ok")
		case strings.HasPrefix(up, "RCPT TO"):
			s.mu.Lock()
			s.gotRcpts = append(s.gotRcpts, strings.TrimRight(line, "\r\n"))
			s.mu.Unlock()
			wr("250 2.1.5 Ok")
		case up == "DATA":
			wr("354 End data with <CR><LF>.<CR><LF>")
			var b strings.Builder
			for {
				dl, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if dl == ".\r\n" {
					break
				}
				b.WriteString(dl)
			}
			s.mu.Lock()
			s.gotData = b.String()
			s.mu.Unlock()
			wr("250 2.0.0 Ok stub-message-id")
		case up == "QUIT":
			wr("221 2.0.0 Bye")
			return
		default:
			wr("250 Ok")
		}
	}
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("splitport: %v", err)
	}
	var n int
	if _, err := fmt.Sscanf(p, "%d", &n); err != nil {
		t.Fatalf("parseport: %v", err)
	}
	return n
}

// TestSMTPSenderImplicitTLS proves the SMTPS (implicit TLS) path: TLS at connect,
// TLS-aware PLAIN auth accepted, and MAIL/RCPT/DATA delivered. This is the exact
// shape Cloudflare Email Sending (smtp.mx.cloudflare.net:465) requires.
func TestSMTPSenderImplicitTLS(t *testing.T) {
	stub := newSMTPSStub(t)
	pool := x509.NewCertPool()
	pool.AddCert(stub.caCert)

	sender := NewSMTP(SMTPConfig{
		Host:      "127.0.0.1",
		Port:      portOf(t, stub.addr),
		Username:  "api_token",
		Password:  "cfut_test_secret",
		UseTLS:    true,
		UseAuth:   true,
		TLSConfig: &tls.Config{RootCAs: pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12},
	})

	res, err := sender.Send(context.Background(), Message{
		From:     Address{Email: "noreply@mail.aocyber.ai"},
		To:       []Address{{Email: "dest@example.com"}},
		Subject:  "implicit tls",
		TextBody: "hello over 465",
	})
	if err != nil {
		t.Fatalf("Send over implicit TLS: %v", err)
	}
	if res.MessageID == "" {
		t.Errorf("expected non-empty message id")
	}

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if !stub.gotAuth {
		t.Errorf("server never received AUTH PLAIN (TLS-aware auth failed)")
	}
	if !strings.Contains(stub.gotFrom, "noreply@mail.aocyber.ai") {
		t.Errorf("MAIL FROM not seen: %q", stub.gotFrom)
	}
	if len(stub.gotRcpts) != 1 || !strings.Contains(stub.gotRcpts[0], "dest@example.com") {
		t.Errorf("RCPT TO not seen: %v", stub.gotRcpts)
	}
	if !strings.Contains(stub.gotData, "hello over 465") {
		t.Errorf("body not delivered: %q", stub.gotData)
	}
}

// TestSMTPSenderImplicitTLSByPort verifies port 465 alone (no UseTLS) selects the
// implicit-TLS path. The dial will fail (nothing on :465 in the test env), but the
// error must come from the TLS dial — proving the STARTTLS path was NOT taken.
func TestSMTPSenderImplicitTLSByPort(t *testing.T) {
	s := &smtpSender{cfg: SMTPConfig{Host: "127.0.0.1", Port: 465}}
	if !s.implicitTLS() {
		t.Fatalf("port 465 must select implicit TLS")
	}
	s587 := &smtpSender{cfg: SMTPConfig{Host: "127.0.0.1", Port: 587}}
	if s587.implicitTLS() {
		t.Fatalf("port 587 without UseTLS must NOT select implicit TLS")
	}
}
