package mailer

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type smtpFixture struct {
	listener    net.Listener
	certificate tls.Certificate
	roots       *x509.CertPool
	mode        string
	reject      string
	messages    chan string
	accepted    chan struct{}
	mu          sync.Mutex
	commands    []string
	workers     sync.WaitGroup
}

func newSMTPFixture(t *testing.T, mode, reject string) *smtpFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "SMTP test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &smtpFixture{
		listener: listener,
		certificate: tls.Certificate{
			Certificate: [][]byte{der},
			PrivateKey:  key,
		},
		roots:    x509.NewCertPool(),
		mode:     mode,
		reject:   reject,
		messages: make(chan string, 8),
		accepted: make(chan struct{}, 8),
	}
	f.roots.AddCert(cert)
	f.workers.Add(1)
	go func() {
		defer f.workers.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			f.workers.Add(1)
			go func() {
				defer f.workers.Done()
				defer conn.Close()
				f.serve(conn)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		f.workers.Wait()
	})
	return f
}

func (f *smtpFixture) serve(conn net.Conn) {
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	f.accepted <- struct{}{}
	if f.mode == "silent" {
		_, _ = conn.Read(make([]byte, 1))
		return
	}
	secure := f.mode == "tls"
	config := &tls.Config{
		Certificates: []tls.Certificate{f.certificate},
		MinVersion:   tls.VersionTLS12,
	}
	if secure {
		conn = tls.Server(conn, config)
	}
	reader := textproto.NewReader(bufio.NewReader(conn))
	_, _ = io.WriteString(conn, "220 test SMTP\r\n")
	for {
		line, err := reader.ReadLine()
		if err != nil {
			return
		}
		command := strings.SplitN(line, " ", 2)[0]
		f.mu.Lock()
		f.commands = append(f.commands, line)
		f.mu.Unlock()
		if command == f.reject {
			_, _ = io.WriteString(conn, "550 private@example.com secret-token smtp-password\r\n")
			return
		}
		switch command {
		case "EHLO":
			if f.mode == "starttls" && !secure {
				_, _ = io.WriteString(conn, "250-test\r\n250 STARTTLS\r\n")
			} else {
				_, _ = io.WriteString(conn, "250-test\r\n250 AUTH PLAIN\r\n")
			}
		case "STARTTLS":
			_, _ = io.WriteString(conn, "220 start TLS\r\n")
			conn = tls.Server(conn, config)
			reader = textproto.NewReader(bufio.NewReader(conn))
			secure = true
		case "AUTH":
			parts := strings.Fields(line)
			if !secure || len(parts) != 3 {
				return
			}
			decoded, err := base64.StdEncoding.DecodeString(parts[2])
			if err != nil || string(decoded) != "\x00smtp-user\x00smtp-password" {
				return
			}
			_, _ = io.WriteString(conn, "235 authenticated\r\n")
		case "MAIL", "RCPT":
			_, _ = io.WriteString(conn, "250 accepted\r\n")
		case "DATA":
			_, _ = io.WriteString(conn, "354 send message\r\n")
			message, err := io.ReadAll(reader.DotReader())
			if err != nil {
				return
			}
			f.messages <- string(message)
			_, _ = io.WriteString(conn, "250 queued\r\n")
		case "QUIT":
			_, _ = io.WriteString(conn, "221 bye\r\n")
			return
		default:
			return
		}
	}
}

func (f *smtpFixture) sender(t *testing.T, mode string, trust bool) *SMTP {
	t.Helper()
	host, value, err := net.SplitHostPort(f.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(value)
	config := Config{
		Host:    host,
		Port:    port,
		TLSMode: mode,
		From:    "学习项目 <noreply@example.com>",
	}
	if mode != "plain" {
		config.Username, config.Password = "smtp-user", "smtp-password"
	}
	sender, err := NewSMTP(config)
	if err != nil {
		t.Fatal(err)
	}
	if trust {
		sender.tlsConfig.RootCAs = f.roots
	}
	return sender
}

func TestSMTPDelivery(t *testing.T) {
	for _, mode := range []string{"plain", "starttls", "tls"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newSMTPFixture(t, mode, "")
			sender := fixture.sender(t, mode, true)
			body := "请确认邮箱：\nhttps://mini.example/auth/register#token=secret-token\n.leading dot\n"
			if err := sender.Send(t.Context(), "recipient@example.com", "验证邮箱", body); err != nil {
				t.Fatal(err)
			}
			message, err := mail.ReadMessage(strings.NewReader(<-fixture.messages))
			if err != nil {
				t.Fatal(err)
			}
			subject, err := (&mime.WordDecoder{}).DecodeHeader(message.Header.Get("Subject"))
			if err != nil || subject != "验证邮箱" {
				t.Fatalf("encoded subject = %q error=%v", subject, err)
			}
			decoded, err := io.ReadAll(quotedprintable.NewReader(message.Body))
			if err != nil || strings.ReplaceAll(string(decoded), "\r\n", "\n") != body {
				t.Fatalf("message body was not preserved: error=%v", err)
			}
			if message.Header.Get("Message-ID") == "" || message.Header.Get("Date") == "" {
				t.Fatal("missing message identity or date")
			}
		})
	}
}

func TestSMTPRejectsDowngradeAndUntrustedCertificate(t *testing.T) {
	for _, tc := range []struct {
		server string
		client string
		trust  bool
	}{
		{"plain", "starttls", true},
		{"tls", "tls", false},
		{"starttls", "starttls", false},
	} {
		t.Run(tc.server+"/"+tc.client+fmt.Sprint(tc.trust), func(t *testing.T) {
			fixture := newSMTPFixture(t, tc.server, "")
			if err := fixture.sender(t, tc.client, tc.trust).Send(t.Context(), "user@example.com", "subject", "secret-token"); err == nil {
				t.Fatal("insecure SMTP connection accepted")
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			for _, command := range fixture.commands {
				if strings.HasPrefix(command, "AUTH") || strings.HasPrefix(command, "MAIL") || strings.HasPrefix(command, "DATA") {
					t.Fatal("credentials or mail sent before TLS was verified")
				}
			}
		})
	}
}

func TestSMTPFailureAndCancellation(t *testing.T) {
	for _, stage := range []string{"AUTH", "RCPT", "DATA"} {
		t.Run(stage, func(t *testing.T) {
			fixture := newSMTPFixture(t, "tls", stage)
			err := fixture.sender(t, "tls", true).Send(t.Context(), "recipient@example.com", "subject", "secret-token")
			if err == nil || !strings.Contains(err.Error(), "550") {
				t.Fatalf("SMTP rejection = %v", err)
			}
			for _, secret := range []string{"private@example.com", "secret-token", "smtp-password"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("SMTP error exposed server reply contents")
				}
			}
		})
	}
	fixture := newSMTPFixture(t, "silent", "")
	sender := fixture.sender(t, "plain", false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sender.Send(ctx, "user@example.com", "subject", "body") }()
	<-fixture.accepted
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled SMTP send = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP greeting ignored context cancellation")
	}
	fixture = newSMTPFixture(t, "plain", "QUIT")
	if err := fixture.sender(t, "plain", false).Send(t.Context(), "user@example.com", "subject", "body"); err != nil {
		t.Fatalf("failed QUIT overturned accepted delivery: %v", err)
	}
}

func TestSMTPInputBoundaries(t *testing.T) {
	for _, config := range []Config{
		{Host: "relay.example.com", Port: 25, TLSMode: "plain", From: "a@example.com"},
		{Host: "127.0.0.1", Port: 25, TLSMode: "plain", From: "a@example.com", Username: "user", Password: "password"},
		{Host: "relay.example.com", Port: 587, TLSMode: "starttls", From: "a@example.com\r\nBcc: victim@example.com"},
		{Host: "relay.example.com", Port: 587, From: "not a mailbox"},
		{Host: "relay.example.com", Port: 587, From: "a@example.com", Password: "password"},
	} {
		if _, err := NewSMTP(config); err == nil {
			t.Fatal("unsafe SMTP configuration accepted")
		}
	}
	sender, err := NewSMTP(Config{
		Host:    "127.0.0.1",
		Port:    1,
		TLSMode: "plain",
		From:    "a@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ to, subject string }{
		{"user@example.com\r\nBcc: victim@example.com", "subject"},
		{"user@example.com", "subject\r\nBcc: victim@example.com"},
	} {
		if err := sender.Send(t.Context(), tc.to, tc.subject, "body"); err == nil || !strings.Contains(err.Error(), "invalid email") {
			t.Fatalf("header injection not rejected before connecting: %v", err)
		}
	}
}
