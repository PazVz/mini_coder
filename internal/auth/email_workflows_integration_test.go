//go:build integration

package auth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PazVz/mini_coder/internal/auth"
	"github.com/PazVz/mini_coder/internal/database/dbgen"
	"github.com/PazVz/mini_coder/internal/testutil"
)

type sentEmail struct {
	to      string
	subject string
	body    string
}

type recordingMail struct {
	mu       sync.Mutex
	messages []sentEmail
	err      error
}

func (m *recordingMail) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, sentEmail{
		to:      to,
		subject: subject,
		body:    body,
	})
	return m.err
}

func (m *recordingMail) last(t *testing.T) sentEmail {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		t.Fatal("no email sent")
	}
	return m.messages[len(m.messages)-1]
}

func emailToken(t *testing.T, message sentEmail) string {
	t.Helper()
	for _, word := range strings.Fields(message.body) {
		if strings.HasPrefix(word, "https://mini.example/") {
			link, err := url.Parse(word)
			if err != nil {
				t.Fatal(err)
			}
			if link.RawQuery != "" || strings.Contains(link.Path, "token") {
				t.Fatal("email proof appeared in the HTTP request target")
			}
			fragment, err := url.ParseQuery(link.Fragment)
			if err != nil || fragment.Get("token") == "" {
				t.Fatalf("email link missing proof: %v", err)
			}
			return fragment.Get("token")
		}
	}
	t.Fatal("email missing trusted confirmation link")
	return ""
}

func newEmailWorkflow(t *testing.T) (*auth.Service, *auth.EmailService, *pgxpool.Pool, *recordingMail) {
	t.Helper()
	accounts, pool := newService(t)
	setup(t, accounts)
	sender := &recordingMail{}
	service, err := auth.NewEmailService(accounts, auth.EmailConfig{
		Sender:              sender,
		PublicURL:           "https://mini.example",
		RegistrationEnabled: true,
		Logger:              slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return accounts, service, pool, sender
}

func allowEmailResend(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), "UPDATE email_delivery_limits SET last_requested_at=clock_timestamp()-interval '2 minutes'"); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationProofLifecycle(t *testing.T) {
	accounts, service, pool, sender := newEmailWorkflow(t)
	ctx := t.Context()
	if err := service.RequestRegistration(ctx, " New+work@EXAMPLE.com "); err != nil {
		t.Fatal(err)
	}
	message := sender.last(t)
	if message.to != "New+work@EXAMPLE.com" {
		t.Fatalf("verification recipient = %q", message.to)
	}
	token := emailToken(t, message)
	hash := sha256.Sum256([]byte(token))
	var stored []byte
	var users int
	if err := pool.QueryRow(ctx, "SELECT token_hash FROM email_actions").Scan(&stored); err != nil || !bytes.Equal(stored, hash[:]) {
		t.Fatalf("stored proof is not its SHA-256 hash: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&users); err != nil || users != 1 {
		t.Fatalf("request created an unverified account: users=%d error=%v", users, err)
	}
	input := auth.RegistrationInput{
		Token:    token,
		Username: "new-user",
		Password: credentials.Password,
	}
	user, err := service.Register(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != message.to || !user.EmailVerified || user.Role != "member" || user.Status != "active" {
		t.Fatalf("registered user = %+v", user)
	}
	var sessions int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id=$1", user.ID).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("registration auto-created a session: %d error=%v", sessions, err)
	}
	if _, err := accounts.Login(ctx, auth.Credentials{
		Username: user.Username,
		Password: input.Password,
	}); err != nil {
		t.Fatalf("registered member cannot log in: %v", err)
	}
	if _, err := service.Register(ctx, input); !errors.Is(err, auth.ErrInvalidEmailToken) {
		t.Fatalf("registration proof replay = %v", err)
	}
	if err := service.RequestRegistration(ctx, "new+WORK@example.com"); !errors.Is(err, auth.ErrEmailRateLimited) {
		t.Fatalf("consumption reset email quota: %v", err)
	}
}

func TestRegistrationRejectsExpiredWrongPurposeAndConflicts(t *testing.T) {
	accounts, service, pool, sender := newEmailWorkflow(t)
	ctx := t.Context()
	session := login(t, accounts)
	if err := service.RequestEmailVerification(ctx, session.User.ID, session.Token); err != nil {
		t.Fatal(err)
	}
	input := auth.RegistrationInput{
		Token:    emailToken(t, sender.last(t)),
		Username: "member",
		Password: credentials.Password,
	}
	if _, err := service.Register(ctx, input); !errors.Is(err, auth.ErrInvalidEmailToken) {
		t.Fatalf("verification token accepted for registration: %v", err)
	}
	if err := service.RequestRegistration(ctx, "member@example.com"); err != nil {
		t.Fatal(err)
	}
	input.Token = emailToken(t, sender.last(t))
	if _, err := service.ConfirmEmail(ctx, session.User.ID, session.Token, input.Token); !errors.Is(err, auth.ErrInvalidEmailToken) {
		t.Fatalf("registration token accepted for account email: %v", err)
	}
	input.Username = credentials.Username
	if _, err := service.Register(ctx, input); !errors.Is(err, auth.ErrUsernameTaken) {
		t.Fatalf("username conflict = %v", err)
	}
	// A failed creation rolls back proof consumption, so the owner can choose a different name.
	input.Username = "member"
	if _, err := service.Register(ctx, input); err != nil {
		t.Fatalf("conflict consumed registration proof: %v", err)
	}
	allowEmailResend(t, pool)
	if err := service.RequestRegistration(ctx, "ADMIN@example.com"); err != nil {
		t.Fatalf("occupied mailbox request disclosed existence: %v", err)
	}
	input.Token, input.Username = emailToken(t, sender.last(t)), "different-user"
	if _, err := service.Register(ctx, input); !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("occupied mailbox registered a second account: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE email_actions SET created_at=clock_timestamp()-interval '2 hours', expires_at=clock_timestamp()-interval '1 hour'"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Register(ctx, input); !errors.Is(err, auth.ErrInvalidEmailToken) {
		t.Fatalf("expired proof accepted: %v", err)
	}
	if err := service.DeleteExpiredEmailState(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM email_actions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("expired proof cleanup: count=%d error=%v", count, err)
	}
}

func TestRegistrationConcurrentConsumption(t *testing.T) {
	_, service, pool, sender := newEmailWorkflow(t)
	if err := service.RequestRegistration(t.Context(), "concurrent@example.com"); err != nil {
		t.Fatal(err)
	}
	input := auth.RegistrationInput{
		Token:    emailToken(t, sender.last(t)),
		Username: "concurrent",
		Password: credentials.Password,
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			<-start
			_, err := service.Register(t.Context(), input)
			results <- err
		})
	}
	close(start)
	group.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, auth.ErrInvalidEmailToken) {
			t.Fatalf("concurrent registration = %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("proof consumed successfully %d times", succeeded)
	}
	var roles int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM user_roles").Scan(&roles); err != nil || roles != 2 {
		t.Fatalf("concurrent registration left partial roles: %d error=%v", roles, err)
	}
}

func TestRegistrationRechecksExpiryAfterWaitingForProof(t *testing.T) {
	_, service, pool, sender := newEmailWorkflow(t)
	ctx := t.Context()
	if err := service.RequestRegistration(ctx, "queued@example.com"); err != nil {
		t.Fatal(err)
	}
	input := auth.RegistrationInput{
		Token:    emailToken(t, sender.last(t)),
		Username: "queued",
		Password: credentials.Password,
	}
	if _, err := pool.Exec(ctx, "UPDATE email_actions SET expires_at=clock_timestamp()+interval '2 seconds'"); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SELECT token_hash FROM email_actions FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := service.Register(ctx, input)
		done <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Second)
	waiting, expired := false, false
	for !expired {
		select {
		case err := <-done:
			t.Fatalf("registration bypassed proof lock: %v", err)
		case <-deadline:
			t.Fatal("registration did not wait for proof expiry")
		case <-ticker.C:
			var blocked bool
			if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '%email_actions%')").Scan(&blocked); err != nil {
				t.Fatal(err)
			}
			waiting = waiting || blocked
			if err := pool.QueryRow(ctx, "SELECT expires_at <= clock_timestamp() FROM email_actions").Scan(&expired); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !waiting {
		t.Fatal("registration never blocked on the proof")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, auth.ErrInvalidEmailToken) {
		t.Fatalf("proof expired while waiting was accepted: %v", err)
	}
}

func TestEmailVerificationChangeAndLegacyBackfill(t *testing.T) {
	accounts, service, pool, sender := newEmailWorkflow(t)
	ctx := t.Context()
	session := login(t, accounts)
	id := session.User.ID
	if err := service.RequestEmailVerification(ctx, id, session.Token); err != nil {
		t.Fatal(err)
	}
	token := emailToken(t, sender.last(t))
	user, err := service.ConfirmEmail(ctx, id, session.Token, token)
	if err != nil || !user.EmailVerified || user.Email != session.User.Email {
		t.Fatalf("verification = %+v error=%v", user, err)
	}
	if err := service.RequestEmailVerification(ctx, id, session.Token); !errors.Is(err, auth.ErrEmailAlreadyVerified) {
		t.Fatalf("already verified = %v", err)
	}
	if _, err := service.ConfirmEmail(ctx, id, session.Token, token); !errors.Is(err, auth.ErrInvalidEmailToken) {
		t.Fatalf("verification replay = %v", err)
	}
	change := auth.EmailChange{
		Email:           "replacement@example.com",
		CurrentPassword: "wrong password",
	}
	if err := service.RequestEmailChange(ctx, id, session.Token, change); !errors.Is(err, auth.ErrInvalidCurrentPassword) {
		t.Fatalf("email change skipped password confirmation: %v", err)
	}
	change.CurrentPassword = credentials.Password
	if err := service.RequestEmailChange(ctx, id, session.Token, change); err != nil {
		t.Fatal(err)
	}
	token = emailToken(t, sender.last(t))
	if current, err := accounts.Authenticate(ctx, session.Token); err != nil || current.Email != user.Email || !current.EmailVerified {
		t.Fatalf("request replaced the primary email prematurely: %+v error=%v", current, err)
	}
	member := createMember(t, accounts, id, "member")
	memberSession, err := accounts.Login(ctx, auth.Credentials{
		Username: member.Username,
		Password: credentials.Password,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmEmail(ctx, member.ID, memberSession.Token, token); !errors.Is(err, auth.ErrInvalidEmailToken) {
		t.Fatalf("another user consumed email change proof: %v", err)
	}
	updated, err := service.ConfirmEmail(ctx, id, session.Token, token)
	if err != nil || updated.Email != change.Email || !updated.EmailVerified {
		t.Fatalf("email replacement = %+v error=%v", updated, err)
	}
	if message := sender.last(t); message.to != user.Email || strings.Contains(message.body, token) {
		t.Fatalf("old address did not receive a safe change notification: destination=%q", message.to)
	}
	if _, err := accounts.Authenticate(ctx, session.Token); err != nil {
		t.Fatalf("email replacement invalidated the current session: %v", err)
	}
	// A pre-email account can add a primary address using the same password-confirmed flow.
	if _, err := pool.Exec(ctx, "UPDATE users SET email=NULL, email_verified_at=NULL WHERE id=$1", member.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.RequestEmailVerification(ctx, member.ID, memberSession.Token); !errors.Is(err, auth.ErrEmailMissing) {
		t.Fatalf("missing primary email = %v", err)
	}
	if err := service.RequestEmailChange(ctx, member.ID, memberSession.Token, auth.EmailChange{
		Email:           "legacy@example.com",
		CurrentPassword: credentials.Password,
	}); err != nil {
		t.Fatal(err)
	}
	filled, err := service.ConfirmEmail(ctx, member.ID, memberSession.Token, emailToken(t, sender.last(t)))
	if err != nil || !filled.EmailVerified || filled.Email != "legacy@example.com" {
		t.Fatalf("legacy email backfill = %+v error=%v", filled, err)
	}
}

func TestEmailChangeConflictAndMutationInvalidateProof(t *testing.T) {
	accounts, service, pool, sender := newEmailWorkflow(t)
	ctx := t.Context()
	session := login(t, accounts)
	member := createMember(t, accounts, session.User.ID, "member")
	if err := service.RequestEmailChange(ctx, session.User.ID, session.Token, auth.EmailChange{
		Email:           member.Email,
		CurrentPassword: credentials.Password,
	}); err != nil {
		t.Fatal(err)
	}
	token := emailToken(t, sender.last(t))
	if _, err := service.ConfirmEmail(ctx, session.User.ID, session.Token, token); !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("email uniqueness conflict = %v", err)
	}
	if current, err := accounts.Authenticate(ctx, session.Token); err != nil || current.Email != session.User.Email {
		t.Fatalf("conflict lost the old primary email: %+v error=%v", current, err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM email_actions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("conflict consumed proof: count=%d error=%v", count, err)
	}
	if err := accounts.ChangePassword(ctx, session.User.ID, session.Token, auth.PasswordChange{
		CurrentPassword: credentials.Password,
		NewPassword:     "replacement password",
	}); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM email_actions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("password change retained email proof: count=%d error=%v", count, err)
	}
	newSession, err := accounts.Login(ctx, auth.Credentials{
		Username: credentials.Username,
		Password: "replacement password",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmEmail(ctx, session.User.ID, newSession.Token, token); !errors.Is(err, auth.ErrInvalidEmailToken) {
		t.Fatalf("old proof survived reauthentication: %v", err)
	}
}

func TestEmailQuotaRotationAndDeliveryFailure(t *testing.T) {
	accounts, service, pool, sender := newEmailWorkflow(t)
	ctx := t.Context()
	var logs bytes.Buffer
	service, err := auth.NewEmailService(accounts, auth.EmailConfig{
		Sender:              sender,
		PublicURL:           "https://mini.example",
		RegistrationEnabled: true,
		Logger:              slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	sender.err = errors.New("SMTP echoed private@example.com and secret-token")
	if err := service.RequestRegistration(ctx, "failure@example.com"); !errors.Is(err, auth.ErrEmailDelivery) {
		t.Fatalf("SMTP failure = %v", err)
	}
	failedToken := emailToken(t, sender.last(t))
	if strings.Contains(logs.String(), "private@example.com") || strings.Contains(logs.String(), "secret-token") || strings.Contains(logs.String(), failedToken) {
		t.Fatal("SMTP failure exposed message contents in logs")
	}
	if _, err := service.Register(ctx, auth.RegistrationInput{
		Token:    failedToken,
		Username: "failure",
		Password: credentials.Password,
	}); !errors.Is(err, auth.ErrInvalidEmailToken) {
		t.Fatalf("failed delivery retained usable proof: %v", err)
	}
	if err := service.RequestRegistration(ctx, "FAILURE@example.com"); !errors.Is(err, auth.ErrEmailRateLimited) {
		t.Fatalf("failed delivery reset quota: %v", err)
	}
	sender.err = nil
	var previous string
	for range 4 {
		allowEmailResend(t, pool)
		if err := service.RequestRegistration(ctx, "failure@example.com"); err != nil {
			t.Fatal(err)
		}
		token := emailToken(t, sender.last(t))
		if token == previous {
			t.Fatal("resend reused a raw token")
		}
		if previous != "" {
			if _, err := service.Register(ctx, auth.RegistrationInput{
				Token:    previous,
				Username: "failure",
				Password: credentials.Password,
			}); !errors.Is(err, auth.ErrInvalidEmailToken) {
				t.Fatalf("resend retained old proof: %v", err)
			}
		}
		previous = token
	}
	allowEmailResend(t, pool)
	err = service.RequestRegistration(ctx, "failure@example.com")
	var limit *auth.EmailRateLimitError
	if !errors.As(err, &limit) || limit.RetryAfter <= 60 {
		t.Fatalf("hourly quota retry time = %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE email_delivery_limits SET window_start=clock_timestamp()-interval '2 hours'"); err != nil {
		t.Fatal(err)
	}
	if err := service.RequestRegistration(ctx, "failure@example.com"); err != nil {
		t.Fatalf("expired quota did not reset: %v", err)
	}
}

func TestRegistrationRequiresInitializedInstance(t *testing.T) {
	pool, provider := testutil.NewDatabase(t)
	if _, err := provider.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	accounts, err := auth.NewService(pool, setupToken)
	if err != nil {
		t.Fatal(err)
	}
	sender := &recordingMail{}
	service, err := auth.NewEmailService(accounts, auth.EmailConfig{
		Sender:              sender,
		PublicURL:           "https://mini.example",
		RegistrationEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RequestRegistration(t.Context(), "first@example.com"); !errors.Is(err, auth.ErrRegistrationDisabled) {
		t.Fatalf("registration opened before administrator setup: %v", err)
	}
	if initialized, err := dbgen.New(pool).GetSetupState(t.Context()); err != nil || initialized {
		t.Fatalf("registration changed setup: %v %v", initialized, err)
	}
}

func TestConcurrentEmailRequestsShareDestinationQuota(t *testing.T) {
	accounts, service, pool, sender := newEmailWorkflow(t)
	other, err := auth.NewEmailService(accounts, auth.EmailConfig{
		Sender:              sender,
		PublicURL:           "https://mini.example",
		RegistrationEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for _, service := range []*auth.EmailService{service, other} {
		group.Go(func() {
			<-start
			results <- service.RequestRegistration(t.Context(), "quota@example.com")
		})
	}
	close(start)
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, auth.ErrEmailRateLimited) {
			t.Fatalf("concurrent email request = %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent quota allowed %d deliveries", successes)
	}
	// Cleanup must preserve a recent cooldown even if the hourly window has ended.
	if _, err := pool.Exec(t.Context(), "UPDATE email_delivery_limits SET window_start=clock_timestamp()-interval '2 hours'"); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteExpiredEmailState(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := service.RequestRegistration(t.Context(), "QUOTA@example.com"); !errors.Is(err, auth.ErrEmailRateLimited) {
		t.Fatalf("quota cleanup bypassed recent cooldown: %v", err)
	}
}

func TestEmailConfirmationRollbackAndQueuedSession(t *testing.T) {
	accounts, service, pool, sender := newEmailWorkflow(t)
	ctx := t.Context()
	session := login(t, accounts)
	if err := service.RequestEmailChange(ctx, session.User.ID, session.Token, auth.EmailChange{
		Email:           "rollback@example.com",
		CurrentPassword: credentials.Password,
	}); err != nil {
		t.Fatal(err)
	}
	token := emailToken(t, sender.last(t))
	_, err := pool.Exec(ctx, `CREATE FUNCTION reject_email() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected email update failure'; END; $$;
		CREATE TRIGGER reject_email BEFORE UPDATE OF email ON users FOR EACH ROW EXECUTE FUNCTION reject_email();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmEmail(ctx, session.User.ID, session.Token, token); err == nil {
		t.Fatal("email confirmation ignored failed update")
	}
	if current, err := accounts.Authenticate(ctx, session.Token); err != nil || current.Email != session.User.Email {
		t.Fatalf("failed update changed primary email: %+v error=%v", current, err)
	}
	if _, err := pool.Exec(ctx, "DROP TRIGGER reject_email ON users; DROP FUNCTION reject_email()"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmEmail(ctx, session.User.ID, session.Token, token); err != nil {
		t.Fatalf("failed update consumed proof: %v", err)
	}
	if err := service.RequestEmailChange(ctx, session.User.ID, session.Token, auth.EmailChange{
		Email:           "waiting@example.com",
		CurrentPassword: credentials.Password,
	}); err != nil {
		t.Fatal(err)
	}
	token = emailToken(t, sender.last(t))
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", session.User.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := service.ConfirmEmail(ctx, session.User.ID, session.Token, token)
		done <- err
	}()
	waitForAuthUserLock(t, pool, done)
	if _, err := tx.Exec(ctx, "UPDATE users SET status='disabled' WHERE id=$1", session.User.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, auth.ErrUnauthenticated) {
		t.Fatalf("waiting email confirmation ignored disabled account: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM email_actions WHERE user_id=$1", session.User.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("disabled account retained email proof: %d error=%v", count, err)
	}
}
