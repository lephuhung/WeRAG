// Package mailer sends plain-text + HTML email over SMTP with the standard
// library only (net/smtp): no connection pool, one connection per message.
// Callers decide when to send; this package never logs the password.
package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Security modes (mirror types.SMTPSecurity*).
const (
	SecurityNone     = "none"
	SecuritySTARTTLS = "starttls"
	SecurityTLS      = "tls"
)

// ErrAuthNeedsTLS: a user name is set but the connection is not encrypted;
// the password is never sent in clear (except to localhost).
var ErrAuthNeedsTLS = errors.New("Máy chủ yêu cầu mã hóa (STARTTLS/TLS) để đăng nhập")

// Config is one SMTP account.
type Config struct {
	Enabled     bool
	Host        string
	Port        int
	Username    string
	Password    string
	FromAddress string
	FromName    string
	Security    string
}

// Ready reports whether mail can be sent: switched on, with a host and a
// sender address.
func (c Config) Ready() bool {
	return c.Enabled && strings.TrimSpace(c.Host) != "" && strings.TrimSpace(c.FromAddress) != ""
}

// Message is one email. To holds bare addresses.
type Message struct {
	To      []string
	Subject string
	Text    string
	HTML    string
}

// Sender delivers a message with an account.
type Sender interface {
	Send(ctx context.Context, cfg Config, msg Message) error
}

// SMTPSender is the net/smtp Sender. Timeout bounds the whole exchange
// (default 30 s); the context deadline wins when it is earlier.
type SMTPSender struct {
	Timeout time.Duration
}

// NewSMTPSender returns the default sender.
func NewSMTPSender() *SMTPSender { return &SMTPSender{Timeout: 30 * time.Second} }

// Send implements Sender.
func (s *SMTPSender) Send(ctx context.Context, cfg Config, msg Message) error {
	if !cfg.Ready() {
		return errors.New("SMTP is not configured")
	}
	if len(msg.To) == 0 {
		return errors.New("no recipient")
	}
	for _, to := range msg.To {
		if err := checkAddress(to); err != nil {
			return err
		}
	}
	if err := checkAddress(cfg.FromAddress); err != nil {
		return fmt.Errorf("sender address: %w", err)
	}
	body, err := BuildMIME(cfg, msg, time.Now())
	if err != nil {
		return err
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	port := cfg.Port
	if port <= 0 {
		port = 587
	}
	host := strings.TrimSpace(cfg.Host)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Deadline: deadline}
	var conn net.Conn
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if cfg.Security == SecurityTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect %s: %w", addr, err)
	}
	_ = conn.SetDeadline(deadline)
	// close the connection when ctx ends first, so a stuck server cannot
	// outlive the caller's deadline
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("SMTP greeting: %w", err)
	}
	defer c.Close()
	if err := c.Hello(localName()); err != nil {
		return fmt.Errorf("EHLO: %w", err)
	}
	if cfg.Security == SecuritySTARTTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("the server does not offer STARTTLS")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if cfg.Username != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			// most servers offer AUTH only once the connection is encrypted
			if cfg.Security != SecuritySTARTTLS && cfg.Security != SecurityTLS {
				return ErrAuthNeedsTLS
			}
			return errors.New("the server does not offer AUTH")
		}
		// smtp.PlainAuth refuses unencrypted connections except to
		// localhost, which is what we want for SecurityNone too.
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, host)); err != nil {
			if strings.Contains(err.Error(), "unencrypted connection") {
				return ErrAuthNeedsTLS
			}
			return fmt.Errorf("AUTH: %w", err)
		}
	}
	if err := c.Mail(cfg.FromAddress); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, to := range msg.To {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", to, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	return c.Quit()
}

func localName() string {
	return "localhost"
}

// checkAddress refuses anything but one bare address (no header injection).
func checkAddress(a string) error {
	if strings.ContainsAny(a, "\r\n") {
		return fmt.Errorf("invalid address %q", a)
	}
	p, err := mail.ParseAddress(a)
	if err != nil || p.Address != strings.TrimSpace(a) {
		return fmt.Errorf("invalid address %q", a)
	}
	return nil
}

// BuildMIME renders msg as a multipart/alternative UTF-8 message (text
// part first, HTML second when present). Recipients go in To.
func BuildMIME(cfg Config, msg Message, now time.Time) ([]byte, error) {
	var b bytes.Buffer
	from := (&mail.Address{Name: sanitizeHeader(cfg.FromName), Address: cfg.FromAddress}).String()
	writeHeader(&b, "From", from)
	writeHeader(&b, "To", strings.Join(msg.To, ", "))
	writeHeader(&b, "Subject", mime.QEncoding.Encode("utf-8", sanitizeHeader(msg.Subject)))
	writeHeader(&b, "Date", now.Format(time.RFC1123Z))
	writeHeader(&b, "Message-ID", "<"+randomHex(16)+"@"+domainOf(cfg.FromAddress)+">")
	writeHeader(&b, "MIME-Version", "1.0")
	if msg.HTML == "" {
		writeHeader(&b, "Content-Type", `text/plain; charset="utf-8"`)
		writeHeader(&b, "Content-Transfer-Encoding", "base64")
		b.WriteString("\r\n")
		writeBase64(&b, msg.Text)
		return b.Bytes(), nil
	}
	boundary := "=_werag_" + randomHex(12)
	writeHeader(&b, "Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	for _, part := range []struct{ ct, body string }{
		{"text/plain", msg.Text},
		{"text/html", msg.HTML},
	} {
		b.WriteString("--" + boundary + "\r\n")
		writeHeader(&b, "Content-Type", part.ct+`; charset="utf-8"`)
		writeHeader(&b, "Content-Transfer-Encoding", "base64")
		b.WriteString("\r\n")
		writeBase64(&b, part.body)
	}
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes(), nil
}

func writeHeader(b *bytes.Buffer, k, v string) {
	b.WriteString(k + ": " + v + "\r\n")
}

func writeBase64(b *bytes.Buffer, s string) {
	enc := base64.StdEncoding.EncodeToString([]byte(s))
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
}

func sanitizeHeader(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(strings.TrimSpace(s))
}

func domainOf(addr string) string {
	if i := strings.LastIndex(addr, "@"); i >= 0 && i < len(addr)-1 {
		return addr[i+1:]
	}
	return "localhost"
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
