package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
	Security string
	Demo     bool
}

type smtpMailer struct{ config SMTPConfig }

func smtpMailerFromEnv(demo bool) (VerificationMailer, SMTPConfig, error) {
	cfg := SMTPConfig{Host: strings.TrimSpace(os.Getenv("SMTP_HOST")), Username: os.Getenv("SMTP_USERNAME"), Password: os.Getenv("SMTP_PASSWORD"), From: strings.TrimSpace(os.Getenv("SMTP_FROM")), FromName: env("SMTP_FROM_NAME", "竞跃"), Security: strings.ToLower(env("SMTP_SECURITY", "starttls")), Demo: demo}
	if cfg.Host == "" {
		if cfg.Username != "" || cfg.Password != "" || cfg.From != "" {
			return nil, cfg, errors.New("SMTP_HOST is required when email credentials or sender are configured")
		}
		return nil, SMTPConfig{}, nil
	}
	var err error
	cfg.Port, err = strconv.Atoi(env("SMTP_PORT", "587"))
	if err != nil {
		return nil, cfg, errors.New("SMTP_PORT must be an integer between 1 and 65535")
	}
	mailer, err := newSMTPMailer(cfg)
	return mailer, cfg, err
}

func newSMTPMailer(cfg SMTPConfig) (*smtpMailer, error) {
	if cfg.Host == "" || strings.ContainsAny(cfg.Host, "\r\n /\\\t") || (strings.Contains(cfg.Host, ":") && net.ParseIP(cfg.Host) == nil) {
		return nil, errors.New("SMTP_HOST must be a hostname or IP without a scheme or port")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, errors.New("SMTP_PORT must be between 1 and 65535")
	}
	address, err := mail.ParseAddress(cfg.From)
	if err != nil || address.Address != cfg.From || strings.ContainsAny(cfg.From, "\r\n") {
		return nil, errors.New("SMTP_FROM must be a valid sender email address")
	}
	if len(cfg.FromName) > 120 || strings.ContainsAny(cfg.FromName, "\r\n") {
		return nil, errors.New("SMTP_FROM_NAME contains invalid characters or is too long")
	}
	if (cfg.Username == "") != (cfg.Password == "") {
		return nil, errors.New("SMTP_USERNAME and SMTP_PASSWORD must either both be set or both be empty")
	}
	switch cfg.Security {
	case "starttls", "tls":
	case "none":
		ip := net.ParseIP(cfg.Host)
		if !cfg.Demo || (cfg.Host != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return nil, errors.New("SMTP_SECURITY=none is only allowed for loopback SMTP in DEMO_MODE=true")
		}
	default:
		return nil, errors.New("SMTP_SECURITY must be starttls or tls (none is restricted to local demos)")
	}
	return &smtpMailer{config: cfg}, nil
}

func (m *smtpMailer) SendVerification(ctx context.Context, recipient, code string) error {
	if _, valid := normalizedEmail(recipient); !valid || !validVerificationCode(code) {
		return errors.New("invalid verification email")
	}
	return m.send(ctx, recipient, verificationMessage(m.config, recipient, code))
}

// A failed final DATA response can mean the server accepted the message but the
// acknowledgement was lost. Bulk retry must not silently duplicate that mail.
type uncertainDeliveryError struct{ cause error }

func (e *uncertainDeliveryError) Error() string { return "SMTP delivery acknowledgement unavailable" }
func (e *uncertainDeliveryError) Unwrap() error { return e.cause }

func (m *smtpMailer) SendMessage(ctx context.Context, recipient, subject, body string) error {
	if _, valid := normalizedEmail(recipient); !valid || strings.TrimSpace(subject) == "" || len(subject) > 200 || strings.ContainsAny(subject, "\r\n") || strings.TrimSpace(body) == "" || len(body) > 50000 {
		return errors.New("invalid notification email")
	}
	return m.send(ctx, recipient, mailMessage(m.config, recipient, subject, body))
}
func (m *smtpMailer) send(ctx context.Context, recipient string, message []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cfg := m.config
	rawConn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return err
	}
	defer rawConn.Close()
	stop := context.AfterFunc(ctx, func() { rawConn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err = rawConn.SetDeadline(deadline); err != nil {
		return err
	}
	if cfg.Security == "none" {
		remote, ok := rawConn.RemoteAddr().(*net.TCPAddr)
		if !ok || !remote.IP.IsLoopback() {
			return errors.New("insecure SMTP connection must terminate on loopback")
		}
	}
	// System trust roots and hostname verification are always used; there is no
	// insecure certificate option or fallback to unencrypted SMTP.
	tlsConfig := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}
	var conn net.Conn = rawConn
	if cfg.Security == "tls" {
		secure := tls.Client(conn, tlsConfig)
		if err = secure.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = secure
	}
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if cfg.Security == "starttls" {
		if available, _ := client.Extension("STARTTLS"); !available {
			return errors.New("SMTP server does not support required STARTTLS")
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if cfg.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return err
		}
	}
	if err = client.Mail(cfg.From); err != nil {
		return err
	}
	if err = client.Rcpt(recipient); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = writer.Write(message); err != nil {
		// Do not terminate DATA after a partial write: the connection closes
		// without the final dot, so the server cannot accept truncated mail.
		return err
	}
	if err = writer.Close(); err != nil {
		return &uncertainDeliveryError{cause: err}
	}
	// DATA was accepted. A failed QUIT must not invalidate an accepted message.
	_ = client.Quit()
	return nil
}

func verificationMessage(cfg SMTPConfig, recipient, code string) []byte {
	body := fmt.Sprintf("你正在注册竞跃协作系统。\r\n\r\n邮箱验证码：%s\r\n\r\n验证码 10 分钟内有效，仅限本次邮箱注册使用。请勿将验证码告诉他人。\r\n如非本人操作，请忽略此邮件。\r\n", code)
	return mailMessage(cfg, recipient, "竞跃 · 邮箱注册验证码", body)
}
func mailMessage(cfg SMTPConfig, recipient, subject, body string) []byte {
	from := (&mail.Address{Name: cfg.FromName, Address: cfg.From}).String()
	to := (&mail.Address{Address: recipient}).String()
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	encodedBody := base64.StdEncoding.EncodeToString([]byte(body))
	var wrapped strings.Builder
	for len(encodedBody) > 76 {
		wrapped.WriteString(encodedBody[:76] + "\r\n")
		encodedBody = encodedBody[76:]
	}
	wrapped.WriteString(encodedBody + "\r\n")
	domain := cfg.From[strings.LastIndex(cfg.From, "@")+1:]
	return []byte("From: " + from + "\r\nTo: " + to + "\r\nSubject: " + mime.BEncoding.Encode("UTF-8", subject) + "\r\nDate: " + time.Now().Format(time.RFC1123Z) + "\r\nMessage-ID: <" + randomID() + "@" + domain + ">\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n" + wrapped.String())
}
