package main

import "testing"

func TestEmailEnvironment(t *testing.T) {
	base := map[string]string{
		"MINICODER_SMTP_HOST":  "smtp.example.com",
		"MINICODER_SMTP_FROM":  "mini_coder <noreply@example.com>",
		"MINICODER_PUBLIC_URL": "https://mini.example.com",
	}
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{"invalid registration switch", "MINICODER_REGISTRATION_ENABLED", "perhaps"},
		{"invalid port", "MINICODER_SMTP_PORT", "smtp"},
		{"out of range port", "MINICODER_SMTP_PORT", "65536"},
		{"missing host", "MINICODER_SMTP_HOST", ""},
		{"invalid sender", "MINICODER_SMTP_FROM", "not a mailbox"},
		{"missing password", "MINICODER_SMTP_USERNAME", "username"},
		{"insecure remote server", "MINICODER_SMTP_TLS", "plain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key == tc.key {
					return tc.value
				}
				return base[key]
			}
			if _, err := emailConfig(getenv); err == nil {
				t.Fatal("invalid SMTP configuration accepted")
			}
		})
	}
	config, err := emailConfig(func(key string) string { return base[key] })
	if err != nil || config.Sender == nil || config.RegistrationEnabled {
		t.Fatalf("default configuration: %+v error=%v", config, err)
	}
	config, err = emailConfig(func(string) string { return "" })
	if err != nil || config.Sender != nil || config.RegistrationEnabled {
		t.Fatalf("disabled configuration: %+v error=%v", config, err)
	}
}
