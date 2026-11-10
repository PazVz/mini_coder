package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/PazVz/mini_coder/internal/database/dbgen"
)

var (
	// ErrEmailUnavailable indicates that outgoing mail is not configured.
	ErrEmailUnavailable = errors.New("email delivery is not configured")
	// ErrRegistrationDisabled indicates that public registration is not open.
	ErrRegistrationDisabled = errors.New("registration is disabled")
	// ErrInvalidEmailToken covers missing, expired, consumed and incorrectly scoped tokens.
	ErrInvalidEmailToken = errors.New("email token is invalid or expired")
	// ErrEmailRateLimited indicates that the destination's delivery quota was exhausted.
	ErrEmailRateLimited = errors.New("email delivery rate limited")
	// ErrEmailDelivery indicates that the SMTP server did not accept the message.
	ErrEmailDelivery = errors.New("email delivery failed")
	// ErrEmailAlreadyVerified indicates that the current address needs no verification.
	ErrEmailAlreadyVerified = errors.New("email is already verified")
	// ErrEmailMissing indicates that a legacy account first needs to add an email.
	ErrEmailMissing = errors.New("account has no email")
)

// MailSender delivers a single plain-text message without logging its contents.
type MailSender interface {
	Send(context.Context, string, string, string) error
}

// EmailConfig supplies delivery, trusted link origin and the public registration switch.
type EmailConfig struct {
	Sender              MailSender
	PublicURL           string
	RegistrationEnabled bool
	Logger              *slog.Logger
}

// EmailService handles email proofs and registration using the existing account service.
type EmailService struct {
	auth                *Service
	sender              MailSender
	publicURL           *url.URL
	registrationEnabled bool
	logger              *slog.Logger
}

// NewEmailService validates configuration without connecting to the SMTP server.
func NewEmailService(accounts *Service, config EmailConfig) (*EmailService, error) {
	s := &EmailService{
		auth:                accounts,
		sender:              config.Sender,
		registrationEnabled: config.RegistrationEnabled,
		logger:              config.Logger,
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	if config.Sender == nil {
		if config.RegistrationEnabled || config.PublicURL != "" {
			return nil, errors.New("email features require SMTP configuration")
		}
		return s, nil
	}
	base, err := url.Parse(config.PublicURL)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.ForceQuery || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("MINICODER_PUBLIC_URL must be an origin without credentials, path, query or fragment")
	}
	local := base.Hostname() == "localhost"
	if ip := net.ParseIP(base.Hostname()); ip != nil {
		local = ip.IsLoopback()
	}
	if base.Scheme != "https" && !(base.Scheme == "http" && local) {
		return nil, errors.New("MINICODER_PUBLIC_URL requires HTTPS, except on loopback")
	}
	s.publicURL = base
	return s, nil
}

// RegistrationInput finishes registration with a mailbox proof and new credentials.
type RegistrationInput struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// EmailChange requests a replacement address after confirming the local password.
type EmailChange struct {
	Email           string `json:"email"`
	CurrentPassword string `json:"current_password"`
}

// EmailRateLimitError reports when the destination's current delivery window ends.
type EmailRateLimitError struct {
	RetryAfter int32
}

// Error describes the exhausted delivery quota.
func (e *EmailRateLimitError) Error() string { return ErrEmailRateLimited.Error() }

// Unwrap supports errors.Is with ErrEmailRateLimited.
func (e *EmailRateLimitError) Unwrap() error { return ErrEmailRateLimited }

func (s *EmailService) checkRegistration(ctx context.Context) error {
	if !s.registrationEnabled {
		return ErrRegistrationDisabled
	}
	if s.sender == nil {
		return ErrEmailUnavailable
	}
	initialized, err := s.auth.queries.GetSetupState(ctx)
	if err != nil {
		return fmt.Errorf("read registration setup state: %w", err)
	}
	if !initialized {
		return ErrRegistrationDisabled
	}
	return nil
}

func validatedEmail(value string) (string, error) {
	email := strings.TrimSpace(value)
	if !validEmail(email) {
		return "", &ValidationError{Fields: []FieldError{{
			Field:  "email",
			Code:   "invalid_format",
			Detail: "邮箱须为单个 ASCII 地址，使用普通本地部分和 DNS 域名，最多 254 字节",
		}}}
	}
	return email, nil
}

// RequestRegistration sends the same proof flow for occupied and unoccupied addresses.
// Account existence is only checked when a mailbox owner consumes the proof.
func (s *EmailService) RequestRegistration(ctx context.Context, value string) error {
	if err := s.checkRegistration(ctx); err != nil {
		return err
	}
	email, err := validatedEmail(value)
	if err != nil {
		return err
	}
	tx, err := s.auth.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin registration email: %w", err)
	}
	defer tx.Rollback(context.Background())
	queries := s.auth.queries.WithTx(tx)
	if err := reserveEmail(ctx, queries, email); err != nil {
		return err
	}
	if err := queries.DeleteRegistrationAction(ctx, email); err != nil {
		return fmt.Errorf("replace registration proof: %w", err)
	}
	token, err := createEmailAction(ctx, queries, "register", pgtype.Int8{}, email)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit registration email: %w", err)
	}
	return s.sendProof(ctx, token, email, "register")
}

// Register atomically consumes a registration proof and creates a verified member.
func (s *EmailService) Register(ctx context.Context, input RegistrationInput) (User, error) {
	if err := s.checkRegistration(ctx); err != nil {
		return User{}, err
	}
	if !validToken(input.Token) {
		return User{}, ErrInvalidEmailToken
	}
	if err := validateCredentials(Credentials{
		Username: input.Username,
		Password: input.Password,
	}); err != nil {
		return User{}, err
	}
	tx, err := s.auth.pool.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin registration: %w", err)
	}
	defer tx.Rollback(context.Background())
	queries := s.auth.queries.WithTx(tx)
	// DELETE can evaluate expiry before waiting for a tuple lock. Lock first, then
	// check clock_timestamp() in a new statement so a queued proof cannot outlive its TTL.
	if _, err := queries.LockRegistrationAction(ctx, tokenHash(input.Token)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrInvalidEmailToken
		}
		return User{}, fmt.Errorf("lock registration proof: %w", err)
	}
	email, err := queries.ConsumeRegistrationAction(ctx, tokenHash(input.Token))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrInvalidEmailToken
	}
	if err != nil {
		return User{}, fmt.Errorf("consume registration proof: %w", err)
	}
	created, err := queries.CreateUser(ctx, dbgen.CreateUserParams{
		Username:     input.Username,
		PasswordHash: pgtype.Text{String: hashPassword(input.Password), Valid: true},
		Email:        pgtype.Text{String: email, Valid: true},
	})
	if err != nil {
		return User{}, fmt.Errorf("create registered user: %w", userCreationError(err))
	}
	if err := queries.SetVerifiedEmail(ctx, dbgen.SetVerifiedEmailParams{
		ID:    created.ID,
		Email: pgtype.Text{String: email, Valid: true},
	}); err != nil {
		return User{}, fmt.Errorf("verify registered email: %w", err)
	}
	user, err := managedUser(ctx, queries, created.ID)
	if err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit registration: %w", err)
	}
	return user, nil
}

// RequestEmailVerification proves the current primary address using an active session.
func (s *EmailService) RequestEmailVerification(ctx context.Context, userID int64, session string) error {
	return s.requestAccountEmail(ctx, userID, session, nil)
}

// RequestEmailChange leaves the old primary address intact until confirmation.
func (s *EmailService) RequestEmailChange(ctx context.Context, userID int64, session string, input EmailChange) error {
	return s.requestAccountEmail(ctx, userID, session, &input)
}

func (s *EmailService) requestAccountEmail(ctx context.Context, userID int64, session string, change *EmailChange) error {
	if s.sender == nil {
		return ErrEmailUnavailable
	}
	tx, queries, user, err := s.auth.beginAccountChange(ctx, userID, session)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	email, purpose := user.Email.String, "verify_email"
	if change == nil {
		if !user.Email.Valid {
			return ErrEmailMissing
		}
		if user.EmailVerifiedAt.Valid {
			return ErrEmailAlreadyVerified
		}
	} else {
		if !utf8.ValidString(change.CurrentPassword) || len(change.CurrentPassword) == 0 || len(change.CurrentPassword) > 1024 {
			return invalidUserField("current_password", "当前密码须为 1～1024 字节的有效 UTF-8 文本")
		}
		if !user.PasswordHash.Valid {
			return ErrPasswordNotSet
		}
		match, err := verifyPassword(user.PasswordHash.String, change.CurrentPassword)
		if err != nil {
			return fmt.Errorf("confirm email change password: %w", err)
		}
		if !match {
			return ErrInvalidCurrentPassword
		}
		email, err = validatedEmail(change.Email)
		if err != nil {
			return err
		}
		purpose = "change_email"
	}
	if err := reserveEmail(ctx, queries, email); err != nil {
		return err
	}
	if err := queries.DeleteUserEmailActions(ctx, pgtype.Int8{Int64: user.ID, Valid: true}); err != nil {
		return fmt.Errorf("replace account email proof: %w", err)
	}
	token, err := createEmailAction(ctx, queries, purpose, pgtype.Int8{Int64: user.ID, Valid: true}, email)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit account email request: %w", err)
	}
	return s.sendProof(ctx, token, email, purpose)
}

// ConfirmEmail consumes a user-bound proof under the account lock and a current session.
func (s *EmailService) ConfirmEmail(ctx context.Context, userID int64, session, token string) (User, error) {
	if s.sender == nil {
		return User{}, ErrEmailUnavailable
	}
	if !validToken(token) {
		return User{}, ErrInvalidEmailToken
	}
	tx, queries, previous, err := s.auth.beginAccountChange(ctx, userID, session)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(context.Background())
	action, err := queries.ConsumeUserEmailAction(ctx, dbgen.ConsumeUserEmailActionParams{
		TokenHash: tokenHash(token),
		UserID:    pgtype.Int8{Int64: userID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrInvalidEmailToken
	}
	if err != nil {
		return User{}, fmt.Errorf("consume account email proof: %w", err)
	}
	if err := queries.SetVerifiedEmail(ctx, dbgen.SetVerifiedEmailParams{
		ID:    userID,
		Email: pgtype.Text{String: action.Email, Valid: true},
	}); err != nil {
		return User{}, fmt.Errorf("update verified email: %w", userCreationError(err))
	}
	user, err := managedUser(ctx, queries, userID)
	if err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit email confirmation: %w", err)
	}
	if previous.Email.Valid && !strings.EqualFold(previous.Email.String, user.Email) {
		// Notification failure cannot roll back a completed identity change.
		if err := s.sender.Send(ctx, previous.Email.String, "mini_coder 主邮箱已修改", "你的 mini_coder 主邮箱已修改。如果不是你本人操作，请联系管理员。"); err != nil {
			s.logger.WarnContext(ctx, "email change notification failed", "user_id", user.ID)
		}
	}
	return user, nil
}

func reserveEmail(ctx context.Context, queries *dbgen.Queries, email string) error {
	_, err := queries.ReserveEmailDelivery(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		seconds, readErr := queries.EmailDeliveryRetryAfter(ctx, email)
		if readErr != nil {
			return fmt.Errorf("read email delivery window: %w", readErr)
		}
		return &EmailRateLimitError{RetryAfter: seconds}
	}
	if err != nil {
		return fmt.Errorf("reserve email delivery: %w", err)
	}
	return nil
}

func createEmailAction(ctx context.Context, queries *dbgen.Queries, purpose string, userID pgtype.Int8, email string) (string, error) {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	if err := queries.CreateEmailAction(ctx, dbgen.CreateEmailActionParams{
		TokenHash: tokenHash(token),
		Purpose:   purpose,
		UserID:    userID,
		Email:     email,
	}); err != nil {
		return "", fmt.Errorf("create email proof: %w", err)
	}
	return token, nil
}

func (s *EmailService) sendProof(ctx context.Context, token, email, purpose string) error {
	path, subject := "email/confirm", "验证 mini_coder 邮箱"
	if purpose == "register" {
		path, subject = "register", "完成 mini_coder 注册"
	}
	link := s.publicURL.JoinPath("auth", path)
	// Fragments stay in the browser, away from access logs and HTTP Referer headers.
	link.Fragment = "token=" + token
	body := "请在 30 分钟内打开以下链接并确认操作：\n\n" + link.String() + "\n\n链接只能使用一次，重新申请后请使用最新邮件。如果不是你本人操作，请忽略此邮件。"
	if err := s.sender.Send(ctx, email, subject, body); err != nil {
		// Never expose transport replies, addresses, message contents or raw tokens in logs.
		s.logger.WarnContext(ctx, "verification email delivery failed", "purpose", purpose)
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.auth.queries.DeleteEmailAction(cleanupCtx, tokenHash(token)); err != nil {
			return fmt.Errorf("invalidate undelivered email proof: %w", err)
		}
		return ErrEmailDelivery
	}
	return nil
}

// DeleteExpiredEmailState removes expired proofs and delivery windows; checks do not depend on cleanup.
func (s *EmailService) DeleteExpiredEmailState(ctx context.Context) error {
	if err := s.auth.queries.DeleteExpiredEmailActions(ctx); err != nil {
		return err
	}
	return s.auth.queries.DeleteExpiredEmailDeliveryLimits(ctx)
}
