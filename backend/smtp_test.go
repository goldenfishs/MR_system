package main

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSMTPSecurityConfiguration(t *testing.T) {
	valid := SMTPConfig{Host: "smtp.example.com", Port: 587, From: "sender@example.com", FromName: "竞跃", Security: "starttls"}
	if _, err := newSMTPMailer(valid); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*SMTPConfig){
		"production plaintext":    func(c *SMTPConfig) { c.Security = "none"; c.Host = "127.0.0.1" },
		"remote demo plaintext":   func(c *SMTPConfig) { c.Security = "none"; c.Demo = true },
		"unknown transport":       func(c *SMTPConfig) { c.Security = "auto" },
		"sender header injection": func(c *SMTPConfig) { c.From = "sender@example.com\r\nBcc: other@example.com" },
		"name header injection":   func(c *SMTPConfig) { c.FromName = "Test\r\nBcc: other@example.com" },
		"invalid port":            func(c *SMTPConfig) { c.Port = 65536 },
		"host with scheme":        func(c *SMTPConfig) { c.Host = "smtp://example.com" },
		"host with port":          func(c *SMTPConfig) { c.Host = "smtp.example.com:25" },
		"partial authentication":  func(c *SMTPConfig) { c.Username = "test" },
	} {
		t.Run(name, func(t *testing.T) {
			c := valid
			mutate(&c)
			if _, err := newSMTPMailer(c); err == nil {
				t.Fatal("unsafe SMTP configuration accepted")
			}
		})
	}
	for _, host := range []string{"127.0.0.1", "::1", "localhost"} {
		c := valid
		c.Host = host
		c.Security = "none"
		c.Demo = true
		if _, err := newSMTPMailer(c); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSMTPEnvironmentConfiguration(t *testing.T) {
	for _, key := range []string{"SMTP_HOST", "SMTP_PORT", "SMTP_FROM", "SMTP_FROM_NAME", "SMTP_USERNAME", "SMTP_PASSWORD", "SMTP_SECURITY"} {
		t.Setenv(key, "")
	}
	mailer, _, err := smtpMailerFromEnv(false)
	if err != nil || mailer != nil {
		t.Fatal("absent SMTP should leave registration disabled")
	}
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_FROM", "sender@example.com")
	mailer, cfg, err := smtpMailerFromEnv(false)
	if err != nil || mailer == nil || cfg.Port != 587 || cfg.Security != "starttls" || cfg.FromName != "QUT机器人战队" {
		t.Fatal("incorrect SMTP defaults")
	}
	t.Setenv("SMTP_PORT", "invalid")
	if _, _, err = smtpMailerFromEnv(false); err == nil {
		t.Fatal("invalid SMTP port accepted")
	}
}

// The local fixture implements only unencrypted SMTP. Requiring STARTTLS must
// fail before sending MAIL or any message to this server.
func localSMTP(t *testing.T) (string, int, <-chan []byte, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	messages := make(chan []byte, 1)
	commands := make(chan string, 20)
	go func() {
		defer close(messages)
		defer close(commands)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		smtp := textproto.NewConn(conn)
		defer smtp.Close()
		if smtp.PrintfLine("220 local test SMTP") != nil {
			return
		}
		for {
			line, err := smtp.ReadLine()
			if err != nil {
				return
			}
			commands <- line
			switch {
			case strings.HasPrefix(line, "EHLO"):
				if smtp.PrintfLine("250-local test SMTP\r\n250 OK") != nil {
					return
				}
			case strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
				if smtp.PrintfLine("250 OK") != nil {
					return
				}
			case line == "DATA":
				if smtp.PrintfLine("354 End with a dot") != nil {
					return
				}
				data, err := smtp.ReadDotBytes()
				if err != nil {
					return
				}
				messages <- data
				if smtp.PrintfLine("250 Accepted") != nil {
					return
				}
			case line == "QUIT":
				smtp.PrintfLine("221 Bye")
				return
			default:
				if smtp.PrintfLine("502 Unsupported") != nil {
					return
				}
			}
		}
	}()
	address := listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", address.Port, messages, commands
}
func TestSMTPLocalDeliveryAndMessageFormat(t *testing.T) {
	host, port, messages, _ := localSMTP(t)
	mailer, err := newSMTPMailer(SMTPConfig{Host: host, Port: port, From: "sender@example.com", FromName: "竞跃", Security: "none", Demo: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = mailer.SendVerification(t.Context(), "learner@example.com", "083729"); err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(<-messages)))
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := mail.ParseAddress(msg.Header.Get("To"))
	if err != nil || recipient.Address != "learner@example.com" {
		t.Fatal("email recipient is incorrect")
	}
	body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, msg.Body))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "083729") || !strings.Contains(string(body), "10 分钟") || msg.Header.Get("Content-Type") != "text/plain; charset=UTF-8" {
		t.Fatal("email code or UTF-8 message formatting missing")
	}
}
func TestSMTPRequiresSTARTTLS(t *testing.T) {
	host, port, _, commands := localSMTP(t)
	mailer, err := newSMTPMailer(SMTPConfig{Host: host, Port: port, From: "sender@example.com", Security: "starttls"})
	if err != nil {
		t.Fatal(err)
	}
	if err = mailer.SendVerification(t.Context(), "learner@example.com", "083729"); err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatal("SMTP silently downgraded encryption")
	}
	for command := range commands {
		if strings.HasPrefix(command, "MAIL") || strings.HasPrefix(command, "AUTH") {
			t.Fatal("SMTP sent content or credentials before encryption")
		}
	}
}
func TestSMTPRejectsUntrustedTLSCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	host, portString, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "https://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatal(err)
	}
	mailer, err := newSMTPMailer(SMTPConfig{Host: host, Port: port, From: "sender@example.com", Security: "tls"})
	if err != nil {
		t.Fatal(err)
	}
	if err = mailer.SendVerification(t.Context(), "learner@example.com", "083729"); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatal("SMTP did not reject untrusted TLS certificate")
	}
}
func TestSMTPCanceledContext(t *testing.T) {
	mailer, err := newSMTPMailer(SMTPConfig{Host: "127.0.0.1", Port: 1, From: "sender@example.com", Security: "none", Demo: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = mailer.SendVerification(ctx, "learner@example.com", "083729"); err == nil {
		t.Fatal("canceled send succeeded")
	}
}
