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
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrAlreadyInitialized = errors.New("already initialized")
	ErrSetupDisabled      = errors.New("setup disabled")
	ErrSetupForbidden     = errors.New("invalid setup token")
	ErrUsernameTaken      = errors.New("username already exists")
	usernamePattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,31}$`)
)

type FieldError struct{ Field, Code, Detail string }
type ValidationError struct{ Fields []FieldError }

func (e *ValidationError) Error() string { return "validation failed" }

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

type Session struct {
	User      User
	Token     string
	ExpiresAt time.Time
}

type Service struct {
	pool       *pgxpool.Pool
	queries    *dbgen.Queries
	setupToken string
	dummyHash  string
}

func NewService(pool *pgxpool.Pool, setupToken string) (*Service, error) {
	if setupToken != "" && len(setupToken) < 32 {
		return nil, errors.New("MINICODER_SETUP_TOKEN must contain at least 32 bytes")
	}
	return &Service{pool: pool, queries: dbgen.New(pool), setupToken: setupToken, dummyHash: hashPassword(rand.Text())}, nil
}

func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	initialized, err := s.queries.GetSetupState(ctx)
	return !initialized, err
}

func validate(c Credentials) error {
	var fields []FieldError
	if !usernamePattern.MatchString(c.Username) {
		fields = append(fields, FieldError{"username", "invalid_format", "用户名须为 3～32 个字符，使用小写字母、数字、下划线或连字符，且以字母或数字开头"})
	}
	if !validPassword(c.Password) {
		fields = append(fields, FieldError{"password", "invalid_length", "密码至少 12 个字符，最多 1024 字节"})
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func (s *Service) Setup(ctx context.Context, token string, c Credentials) (User, error) {
	if s.setupToken == "" {
		return User{}, ErrSetupDisabled
	}
	expected, supplied := sha256.Sum256([]byte(s.setupToken)), sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(expected[:], supplied[:]) != 1 {
		return User{}, ErrSetupForbidden
	}
	if err := validate(c); err != nil {
		return User{}, err
	}
	hash := hashPassword(c.Password)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin setup: %w", err)
	}
	defer tx.Rollback(context.Background())
	q := s.queries.WithTx(tx)
	initialized, err := q.LockSetupState(ctx)
	if err != nil {
		return User{}, fmt.Errorf("lock setup: %w", err)
	}
	if initialized {
		return User{}, ErrAlreadyInitialized
	}
	u, err := q.CreateUser(ctx, dbgen.CreateUserParams{Username: c.Username, PasswordHash: hash})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_username_key" {
			return User{}, ErrUsernameTaken
		}
		return User{}, fmt.Errorf("create initial user: %w", err)
	}
	if err := q.SetAdminRole(ctx, u.ID); err != nil {
		return User{}, fmt.Errorf("assign admin: %w", err)
	}
	if err := q.CompleteSetup(ctx); err != nil {
		return User{}, fmt.Errorf("complete setup: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit setup: %w", err)
	}
	return User{u.ID, u.Username, u.Status, "admin", u.CreatedAt, u.UpdatedAt}, nil
}

func (s *Service) Login(ctx context.Context, c Credentials) (Session, error) {
	if err := validate(c); err != nil {
		return Session{}, err
	}
	u, err := s.queries.GetAuthUserByUsername(ctx, c.Username)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Session{}, fmt.Errorf("find login user: %w", err)
	}
	hash := u.PasswordHash
	if errors.Is(err, pgx.ErrNoRows) {
		hash = s.dummyHash
	}
	match, err := verifyPassword(hash, c.Password)
	if err != nil {
		return Session{}, fmt.Errorf("verify password: %w", err)
	}
	if !match || u.ID == 0 || u.Status != "active" {
		return Session{}, ErrInvalidCredentials
	}

	// Serialize session creation with disabling the user. The disable trigger then
	// cannot miss an in-flight insert, including across multiple server processes.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("begin login: %w", err)
	}
	defer tx.Rollback(context.Background())
	q := s.queries.WithTx(tx)
	current, err := q.LockAuthUserByID(ctx, u.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, fmt.Errorf("lock login user: %w", err)
	}
	if current.Status != "active" || current.PasswordHash != u.PasswordHash {
		return Session{}, ErrInvalidCredentials
	}
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires, err := q.CreateSession(ctx, dbgen.CreateSessionParams{TokenHash: tokenHash(token), UserID: u.ID})
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, fmt.Errorf("commit login: %w", err)
	}
	return Session{User: User{current.ID, current.Username, current.Status, current.Role, current.CreatedAt, current.UpdatedAt}, Token: token, ExpiresAt: expires}, nil
}

func tokenHash(token string) []byte { sum := sha256.Sum256([]byte(token)); return sum[:] }
func validToken(token string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) == 32 && base64.RawURLEncoding.EncodeToString(raw) == token
}

func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if !validToken(token) {
		return User{}, ErrUnauthenticated
	}
	u, err := s.queries.GetSessionUser(ctx, tokenHash(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUnauthenticated
	}
	if err != nil {
		return User{}, fmt.Errorf("read session: %w", err)
	}
	return User{u.ID, u.Username, u.Status, u.Role, u.CreatedAt, u.UpdatedAt}, nil
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

func (s *Service) DeleteExpiredSessions(ctx context.Context) error {
	return s.queries.DeleteExpiredSessions(ctx)
}
