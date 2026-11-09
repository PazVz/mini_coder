// Package auth implements password authentication and persistent browser sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PazVz/mini_coder/internal/database/dbgen"
)

var (
	// ErrInvalidCredentials covers unknown users, password mismatches and disabled accounts.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrUnauthenticated indicates a missing, invalid, expired or revoked session.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrAlreadyInitialized indicates that the initial administrator was already created.
	ErrAlreadyInitialized = errors.New("already initialized")
	// ErrSetupDisabled indicates that no setup token is configured.
	ErrSetupDisabled = errors.New("setup disabled")
	// ErrSetupForbidden indicates that the supplied setup token does not match.
	ErrSetupForbidden = errors.New("invalid setup token")
	// ErrUsernameTaken indicates that another user has the requested username.
	ErrUsernameTaken = errors.New("username already exists")
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,31}$`)

// FieldError describes a rejected credential field using a stable code and a readable detail.
type FieldError struct {
	Field  string
	Code   string
	Detail string
}

// ValidationError collects credential errors so callers can report them together.
type ValidationError struct{ Fields []FieldError }

// Error implements error for credential validation failures.
func (e *ValidationError) Error() string { return "validation failed" }

// Credentials contains the username and password supplied for setup or login.
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// User is an explicit public representation; database hashes never reach HTTP.
// IDs are decimal strings so browser clients don't lose bigint precision.
type User struct {
	ID        int64     `json:"id,string"`
	Username  string    `json:"username"`
	Status    string    `json:"status"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Session is a newly issued login session. Token is returned only through the session cookie.
type Session struct {
	User      User
	Token     string
	ExpiresAt time.Time
}

// Service manages administrator setup, password login and persistent sessions.
type Service struct {
	pool       *pgxpool.Pool
	queries    *dbgen.Queries
	setupToken string
	dummyHash  string
}

// NewService uses the caller-owned pool; an empty setupToken disables setup writes.
func NewService(pool *pgxpool.Pool, setupToken string) (*Service, error) {
	if setupToken != "" && len(setupToken) < 32 {
		return nil, errors.New("MINICODER_SETUP_TOKEN must contain at least 32 bytes")
	}
	return &Service{
		pool:       pool,
		queries:    dbgen.New(pool),
		setupToken: setupToken,
		dummyHash:  hashPassword(rand.Text()),
	}, nil
}

// NeedsSetup reports the persistent setup state, independently of whether setup is enabled.
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	initialized, err := s.queries.GetSetupState(ctx)
	return !initialized, err
}

func validateCredentials(credentials Credentials) error {
	var fields []FieldError
	if !usernamePattern.MatchString(credentials.Username) {
		fields = append(fields, FieldError{
			Field:  "username",
			Code:   "invalid_format",
			Detail: "用户名须为 3～32 个字符，使用小写字母、数字、下划线或连字符，且以字母或数字开头",
		})
	}
	if !validPassword(credentials.Password) {
		fields = append(fields, FieldError{
			Field:  "password",
			Code:   "invalid_length",
			Detail: "密码至少 12 个字符，最多 1024 字节",
		})
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// Setup creates the initial administrator and completes setup in one transaction.
func (s *Service) Setup(ctx context.Context, token string, credentials Credentials) (User, error) {
	if s.setupToken == "" {
		return User{}, ErrSetupDisabled
	}
	expected, supplied := sha256.Sum256([]byte(s.setupToken)), sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(expected[:], supplied[:]) != 1 {
		return User{}, ErrSetupForbidden
	}
	if err := validateCredentials(credentials); err != nil {
		return User{}, err
	}
	passwordHash := hashPassword(credentials.Password)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin setup: %w", err)
	}
	// Rollback still runs after request cancellation; after commit it is harmless.
	defer tx.Rollback(context.Background())
	txQueries := s.queries.WithTx(tx)
	// The permanent auth_setup singleton serializes setup even if users are later deleted.
	initialized, err := txQueries.LockSetupState(ctx)
	if err != nil {
		return User{}, fmt.Errorf("lock setup: %w", err)
	}
	if initialized {
		return User{}, ErrAlreadyInitialized
	}
	// CreateUser also assigns the default member role in database/queries/users.sql.
	user, err := txQueries.CreateUser(ctx, dbgen.CreateUserParams{
		Username:     credentials.Username,
		PasswordHash: passwordHash,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_username_key" {
			return User{}, ErrUsernameTaken
		}
		return User{}, fmt.Errorf("create initial user: %w", err)
	}
	if err := txQueries.SetAdminRole(ctx, user.ID); err != nil {
		return User{}, fmt.Errorf("assign admin: %w", err)
	}
	if err := txQueries.CompleteSetup(ctx); err != nil {
		return User{}, fmt.Errorf("complete setup: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit setup: %w", err)
	}
	return User{
		ID:        user.ID,
		Username:  user.Username,
		Status:    user.Status,
		Role:      "admin",
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}, nil
}

// Login verifies credentials and creates a separate session for each successful login.
func (s *Service) Login(ctx context.Context, credentials Credentials) (Session, error) {
	if err := validateCredentials(credentials); err != nil {
		return Session{}, err
	}
	user, err := s.queries.GetAuthUserByUsername(ctx, credentials.Username)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Session{}, fmt.Errorf("find login user: %w", err)
	}
	passwordHash := user.PasswordHash
	if errors.Is(err, pgx.ErrNoRows) {
		// Unknown users still incur the same password hashing work.
		passwordHash = s.dummyHash
	}
	match, err := verifyPassword(passwordHash, credentials.Password)
	if err != nil {
		return Session{}, fmt.Errorf("verify password: %w", err)
	}
	if !match || user.ID == 0 || user.Status != "active" {
		return Session{}, ErrInvalidCredentials
	}

	// The user row lock in database/queries/auth.sql serializes session creation
	// with disabling the user. The users_revoke_sessions trigger in migration
	// 00002_auth.sql then cannot miss an in-flight insert, even across processes.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("begin login: %w", err)
	}
	defer tx.Rollback(context.Background())
	txQueries := s.queries.WithTx(tx)
	currentUser, err := txQueries.LockAuthUserByID(ctx, user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, fmt.Errorf("lock login user: %w", err)
	}
	// Credentials may have changed while the password was being verified.
	if currentUser.Status != "active" || currentUser.PasswordHash != user.PasswordHash {
		return Session{}, ErrInvalidCredentials
	}
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt, err := txQueries.CreateSession(ctx, dbgen.CreateSessionParams{
		TokenHash: tokenHash(token),
		UserID:    user.ID,
	})
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, fmt.Errorf("commit login: %w", err)
	}
	return Session{
		User: User{
			ID:        currentUser.ID,
			Username:  currentUser.Username,
			Status:    currentUser.Status,
			Role:      currentUser.Role,
			CreatedAt: currentUser.CreatedAt,
			UpdatedAt: currentUser.UpdatedAt,
		},
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

func tokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func validToken(token string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == token
}

// Authenticate resolves an active session to the user's current status and role.
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if !validToken(token) {
		return User{}, ErrUnauthenticated
	}
	// database/queries/auth.sql filters expired sessions and disabled users.
	// Expiry takes effect immediately, independently of the background cleanup.
	user, err := s.queries.GetSessionUser(ctx, tokenHash(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUnauthenticated
	}
	if err != nil {
		return User{}, fmt.Errorf("read session: %w", err)
	}
	return User{
		ID:        user.ID,
		Username:  user.Username,
		Status:    user.Status,
		Role:      user.Role,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}, nil
}

// Logout is idempotent even after expiry or prior revocation.
func (s *Service) Logout(ctx context.Context, token string) error {
	if !validToken(token) {
		return nil
	}
	if err := s.queries.DeleteSession(ctx, tokenHash(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes expired rows; Authenticate does not depend on this cleanup.
func (s *Service) DeleteExpiredSessions(ctx context.Context) error {
	return s.queries.DeleteExpiredSessions(ctx)
}
