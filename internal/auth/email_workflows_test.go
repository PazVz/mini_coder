package auth

import (
	"context"
	"errors"
	"testing"
)

type unusedMailSender struct{}

func (unusedMailSender) Send(context.Context, string, string, string) error { return nil }

func TestEmailServiceConfiguration(t *testing.T) {
	for _, tc := range []struct {
		url   string
		valid bool
	}{
		{"https://mini.example", true},
		{"https://mini.example/", true},
		{"http://127.0.0.1:8080", true},
		{"http://[::1]:8080", true},
		{"http://localhost:8080", true},
		{"", false},
		{"http://mini.example", false},
		{"https://mini.example/path", false},
		{"https://mini.example?", false},
		{"https://mini.example?url=evil", false},
		{"https://mini.example#token=bad", false},
		{"https://user:password@mini.example", false},
		{"//mini.example", false},
		{"https://mini.example:invalid", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			_, err := NewEmailService(nil, EmailConfig{
				Sender:    unusedMailSender{},
				PublicURL: tc.url,
			})
			if (err == nil) != tc.valid {
				t.Fatalf("configuration accepted = %v, want %v", err == nil, tc.valid)
			}
		})
	}
	if _, err := NewEmailService(nil, EmailConfig{RegistrationEnabled: true}); err == nil {
		t.Fatal("registration enabled without SMTP")
	}
	disabled, err := NewEmailService(nil, EmailConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := disabled.RequestRegistration(t.Context(), "a@example.com"); !errors.Is(err, ErrRegistrationDisabled) {
		t.Fatalf("disabled registration = %v", err)
	}
	if err := disabled.RequestEmailVerification(t.Context(), 1, ""); !errors.Is(err, ErrEmailUnavailable) {
		t.Fatalf("disabled mail = %v", err)
	}
}
