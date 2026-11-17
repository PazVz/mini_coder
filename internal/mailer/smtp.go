// Package mailer sends bounded, TLS-protected transactional email through SMTP.
package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// Config contains only trusted operator settings. Plain mode is restricted to loopback.
type Config struct {
	Host     string
	Port     int
	TLSMode  string
	Username string
	Password string
	From     string
}

// SMTP delivers one message per connection with a five-second total deadline.
type SMTP struct {
	config    Config
	from      *mail.Address
	tlsConfig *tls.Config
}

// NewSMTP validates settings without opening a connection or sending a message.
func NewSMTP(config Config) (*SMTP, error) {
	if config.Host == "" || strings.ContainsAny(config.Host, "\r\n /\t") || config.Port < 1 || config.Port > 65535 {
		return nil, errors.New("SMTP host and port must be valid")
	}
	if (config.Username == "") != (config.Password == "") {
		return nil, errors.New("SMTP username and password must be configured together")
	}
	if config.TLSMode == "" {
		config.TLSMode = "starttls"
	}
	switch config.TLSMode {
	case "starttls", "tls":
	case "plain":
		ip := net.ParseIP(config.Host)
		if ip == nil || !ip.IsLoopback() || config.Username != "" {
			return nil, errors.New("plain SMTP is only allowed on a loopback IP without credentials")
		}
	default:
		return nil, errors.New("SMTP TLS mode must be starttls, tls or plain")
	}
	from, err := mail.ParseAddress(config.From)
	if err != nil || strings.ContainsAny(config.From, "\r\n") || !asciiMailbox(from.Address) {
		return nil, errors.New("SMTP From must contain one valid ASCII mailbox")
	}
	return &SMTP{
		config: config,
		from:   from,
		tlsConfig: &tls.Config{
			ServerName: config.Host,
			MinVersion: tls.VersionTLS12,
		},
	}, nil
}

func asciiMailbox(value string) bool {
	for _, char := range value {
		if char < '!' || char > '~' {
			return false
		}
	}
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Name == "" && parsed.Address == value
}

// Send waits for DATA acceptance. It does not retry ambiguous delivery failures.
func (s *SMTP) Send(ctx context.Context, to, subject, body string) error {
	if !asciiMailbox(to) || strings.ContainsAny(subject, "\r\n") {
		return errors.New("invalid email destination or subject")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port)))
	if err != nil {
		return deliveryError(ctx, "connect", err)
	}
	defer conn.Close()
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return deliveryError(ctx, "deadline", err)
	}
	var transport net.Conn = conn
	if s.config.TLSMode == "tls" {
		secure := tls.Client(conn, s.tlsConfig)
		if err := secure.HandshakeContext(ctx); err != nil {
			return deliveryError(ctx, "TLS", err)
		}
		transport = secure
	}
	client, err := smtp.NewClient(transport, s.config.Host)
	if err != nil {
		return deliveryError(ctx, "greeting", err)
	}
	defer client.Close()
	if s.config.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not offer required STARTTLS")
		}
		if err := client.StartTLS(s.tlsConfig); err != nil {
			return deliveryError(ctx, "STARTTLS", err)
		}
	}
	if s.config.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)); err != nil {
			return deliveryError(ctx, "authentication", err)
		}
	}
	if err := client.Mail(s.from.Address); err != nil {
		return deliveryError(ctx, "sender", err)
	}
	if err := client.Rcpt(to); err != nil {
		return deliveryError(ctx, "recipient", err)
	}
	data, err := client.Data()
	if err != nil {
		return deliveryError(ctx, "DATA", err)
	}
	if _, err := data.Write(s.message(to, subject, body)); err != nil {
		return deliveryError(ctx, "message", err)
	}
	if err := data.Close(); err != nil {
		return deliveryError(ctx, "acceptance", err)
	}
	// A failed QUIT cannot undo the server's final 250 response to DATA.
	_ = client.Quit()
	return nil
}

func (s *SMTP) message(to, subject, body string) []byte {
	var message bytes.Buffer
	_, domain, _ := strings.Cut(s.from.Address, "@")
	fmt.Fprintf(&message, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n",
		s.from.String(), (&mail.Address{Address: to}).String(), mime.QEncoding.Encode("utf-8", subject), time.Now().Format(time.RFC1123Z), rand.Text(), domain)
	encoded := quotedprintable.NewWriter(&message)
	// bytes.Buffer never fails; quotedprintable also canonicalizes body line endings.
	_, _ = io.WriteString(encoded, body)
	_ = encoded.Close()
	return message.Bytes()
}

// SMTP replies may echo credentials or message content. Retain the stage and status only.
func deliveryError(ctx context.Context, stage string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var reply *textproto.Error
	if errors.As(err, &reply) {
		return fmt.Errorf("SMTP %s failed (status %d)", stage, reply.Code)
	}
	return fmt.Errorf("SMTP %s failed", stage)
}
