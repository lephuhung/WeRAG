package mailer

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestBuildMIMEAlternativeUTF8(t *testing.T) {
	cfg := Config{FromAddress: "no-reply@example.com", FromName: "Hệ thống\r\nBcc: x@y.z"}
	msg := Message{To: []string{"a@example.com"}, Subject: "Gói sắp hết hạn", Text: "Xin chào", HTML: "<p>Xin chào</p>"}
	raw, err := BuildMIME(cfg, msg, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	head := s[:strings.Index(s, "\r\n\r\n")]
	if strings.Contains(head, "\nBcc:") {
		t.Fatalf("header injection through the sender name:\n%s", head)
	}
	if !strings.Contains(head, "Subject: =?utf-8?q?") {
		t.Fatalf("subject not encoded:\n%s", head)
	}
	if !strings.Contains(s, "multipart/alternative") || !strings.Contains(s, "text/html") {
		t.Fatalf("missing parts:\n%s", s)
	}
	if !strings.Contains(s, base64.StdEncoding.EncodeToString([]byte("Xin chào"))) {
		t.Fatal("text part not base64")
	}
}

func TestSendRefusesBadInputBeforeConnecting(t *testing.T) {
	s := &SMTPSender{Timeout: time.Second}
	ok := Config{Enabled: true, Host: "127.0.0.1", Port: 1, FromAddress: "no-reply@example.com"}
	if err := s.Send(context.Background(), Config{}, Message{To: []string{"a@example.com"}}); err == nil {
		t.Fatal("unconfigured SMTP must fail")
	}
	if err := s.Send(context.Background(), ok, Message{To: []string{"a@example.com\r\nRCPT TO:<b@x.y>"}}); err == nil ||
		!strings.Contains(err.Error(), "invalid address") {
		t.Fatalf("CRLF recipient: %v", err)
	}
	if err := s.Send(context.Background(), ok, Message{}); err == nil {
		t.Fatal("no recipient must fail")
	}
}

// A minimal SMTP server: the plain (no TLS, no AUTH) exchange delivers the
// message.
func TestSendPlainSMTP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		w("220 test ESMTP")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					w("250 queued")
					got <- data.String()
					continue
				}
				data.WriteString(line)
				continue
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"):
				w("250-test")
				w("250 8BITMIME")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				w("250 ok")
			case cmd == "DATA":
				inData = true
				w("354 go")
			case cmd == "QUIT":
				w("221 bye")
				return
			default:
				w("502 ?")
			}
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	cfg := Config{Enabled: true, Host: "127.0.0.1", Port: port, FromAddress: "no-reply@example.com", Security: SecurityNone}
	err = NewSMTPSender().Send(context.Background(), cfg,
		Message{To: []string{"a@example.com"}, Subject: "Thử", Text: "Xin chào", HTML: "<p>Xin chào</p>"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-got:
		if !strings.Contains(body, "To: a@example.com") {
			t.Fatalf("body:\n%s", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message delivered")
	}
}

// A user name over an unencrypted connection to a server that offers no
// AUTH there gives the clear message, not "no AUTH".
func TestSendNoneWithUsernameNeedsTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		_, _ = c.Write([]byte("220 test ESMTP\r\n"))
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(strings.ToUpper(line), "EHLO") {
				_, _ = c.Write([]byte("250-test\r\n250 STARTTLS\r\n"))
			} else {
				_, _ = c.Write([]byte("221 bye\r\n"))
			}
		}
	}()
	cfg := Config{Enabled: true, Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Username: "u", Password: "p",
		FromAddress: "no-reply@example.com", Security: SecurityNone}
	err = NewSMTPSender().Send(context.Background(), cfg, Message{To: []string{"a@example.com"}, Text: "x"})
	if !errors.Is(err, ErrAuthNeedsTLS) {
		t.Fatalf("err = %v", err)
	}
}
