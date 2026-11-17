package main

import (
	"errors"
	"log/slog"
	"os"
	"strconv"

	"github.com/PazVz/mini_coder/internal/auth"
	"github.com/PazVz/mini_coder/internal/mailer"
)

func emailConfig(getenv func(string) string) (auth.EmailConfig, error) {
	config := auth.EmailConfig{PublicURL: getenv("MINICODER_PUBLIC_URL")}
	if value := getenv("MINICODER_REGISTRATION_ENABLED"); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return config, errors.New("MINICODER_REGISTRATION_ENABLED must be a boolean")
		}
		config.RegistrationEnabled = enabled
	}
	host := getenv("MINICODER_SMTP_HOST")
	if host == "" {
		for _, name := range []string{"MINICODER_SMTP_PORT", "MINICODER_SMTP_TLS", "MINICODER_SMTP_USERNAME", "MINICODER_SMTP_PASSWORD", "MINICODER_SMTP_FROM"} {
			if getenv(name) != "" {
				return config, errors.New("SMTP settings require MINICODER_SMTP_HOST")
			}
		}
		return config, nil
	}
	port := 587
	if value := getenv("MINICODER_SMTP_PORT"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return config, errors.New("MINICODER_SMTP_PORT must be a port number")
		}
		port = parsed
	}
	sender, err := mailer.NewSMTP(mailer.Config{
		Host:     host,
		Port:     port,
		TLSMode:  getenv("MINICODER_SMTP_TLS"),
		Username: getenv("MINICODER_SMTP_USERNAME"),
		Password: getenv("MINICODER_SMTP_PASSWORD"),
		From:     getenv("MINICODER_SMTP_FROM"),
	})
	if err != nil {
		return config, err
	}
	config.Sender = sender
	return config, nil
}

func newEmailService(accounts *auth.Service, logger *slog.Logger) (*auth.EmailService, error) {
	config, err := emailConfig(os.Getenv)
	if err != nil {
		return nil, err
	}
	config.Logger = logger
	return auth.NewEmailService(accounts, config)
}
