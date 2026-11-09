package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/PazVz/mini_coder/internal/authz"
	"github.com/PazVz/mini_coder/internal/database/dbgen"
)

var (
	// ErrUserNotFound indicates that the requested user does not exist.
	ErrUserNotFound = errors.New("user not found")
	// ErrLastAdmin protects the last active administrator from removal.
	ErrLastAdmin = errors.New("at least one active administrator is required")
)

// ListUsersOptions selects an ID-ordered page after an exclusive cursor.
type ListUsersOptions struct {
	AfterID int64
	Limit   int32
}

// UserPage contains public users and an optional cursor for the next page.
type UserPage struct {
	Users       []User `json:"users"`
	NextAfterID string `json:"next_after_id,omitempty"`
}

// CreateUser creates an active member after checking the current actor's permissions.
// actorID must come from a trusted authentication boundary, never the request body.
func (s *Service) CreateUser(ctx context.Context, actorID int64, input CreateUserInput) (User, error) {
	tx, queries, err := s.beginUserManagement(ctx, actorID, authz.UserCreate)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(context.Background())
	email, err := validateCreateUser(input)
	if err != nil {
		return User{}, err
	}
	created, err := queries.CreateUser(ctx, dbgen.CreateUserParams{
		Username:     input.Username,
		PasswordHash: pgtype.Text{String: hashPassword(input.Password), Valid: true},
		Email:        pgtype.Text{String: email, Valid: true},
	})
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", userCreationError(err))
	}
	user, err := managedUser(ctx, queries, created.ID)
	if err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit user creation: %w", err)
	}
	return user, nil
}

// ListUsers returns a bounded page after checking the actor's current role and status.
func (s *Service) ListUsers(ctx context.Context, actorID int64, options ListUsersOptions) (UserPage, error) {
	if err := authorizeUserManagement(ctx, s.queries, actorID, authz.UserList); err != nil {
		return UserPage{}, err
	}
	if options.Limit < 1 || options.Limit > 100 {
		return UserPage{}, invalidUserField("limit", "须为 1～100 的整数")
	}
	if options.AfterID < 0 {
		return UserPage{}, invalidUserField("after_id", "须为非负整数 ID")
	}
	rows, err := s.queries.ListManagedUsers(ctx, dbgen.ListManagedUsersParams{
		AfterID:   options.AfterID,
		PageLimit: options.Limit + 1,
	})
	if err != nil {
		return UserPage{}, fmt.Errorf("list users: %w", err)
	}
	page := UserPage{Users: make([]User, 0, len(rows))}
	for i, row := range rows {
		if i == int(options.Limit) {
			page.NextAfterID = strconv.FormatInt(page.Users[i-1].ID, 10)
			break
		}
		page.Users = append(page.Users, publicManagedUser(dbgen.GetManagedUserRow(row)))
	}
	return page, nil
}

// UpdateUserStatus changes account availability and preserves an active administrator.
// Disabling also revokes sessions through the trigger updated in migration 00004.
func (s *Service) UpdateUserStatus(ctx context.Context, actorID, userID int64, status string) (User, error) {
	tx, queries, err := s.beginUserManagement(ctx, actorID, authz.UserUpdateStatus)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(context.Background())
	if status != "active" && status != "disabled" {
		return User{}, invalidUserField("status", "须为 active 或 disabled")
	}
	user, err := managedUser(ctx, queries, userID)
	if err != nil {
		return User{}, err
	}
	if user.Status != status {
		if err := preserveActiveAdmin(ctx, queries, user, status, user.Role); err != nil {
			return User{}, err
		}
		if err := queries.UpdateUserStatus(ctx, dbgen.UpdateUserStatusParams{
			ID:     userID,
			Status: status,
		}); err != nil {
			return User{}, fmt.Errorf("update user status: %w", err)
		}
		user, err = managedUser(ctx, queries, userID)
		if err != nil {
			return User{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit user status: %w", err)
	}
	return user, nil
}

// UpdateUserRole assigns a built-in role; subsequent requests use the new permission set.
func (s *Service) UpdateUserRole(ctx context.Context, actorID, userID int64, role string) (User, error) {
	tx, queries, err := s.beginUserManagement(ctx, actorID, authz.UserUpdateRole)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(context.Background())
	if role != "admin" && role != "member" {
		return User{}, invalidUserField("role", "须为 admin 或 member")
	}
	user, err := managedUser(ctx, queries, userID)
	if err != nil {
		return User{}, err
	}
	if user.Role != role {
		if err := preserveActiveAdmin(ctx, queries, user, user.Status, role); err != nil {
			return User{}, err
		}
		if err := queries.UpdateUserRole(ctx, dbgen.UpdateUserRoleParams{
			UserID: userID,
			Role:   role,
		}); err != nil {
			return User{}, fmt.Errorf("update user role: %w", err)
		}
		user, err = managedUser(ctx, queries, userID)
		if err != nil {
			return User{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit user role: %w", err)
	}
	return user, nil
}

func (s *Service) beginUserManagement(ctx context.Context, actorID int64, permission authz.Permission) (pgx.Tx, *dbgen.Queries, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, nil, fmt.Errorf("begin user management: %w", err)
	}
	queries := s.queries.WithTx(tx)
	// Take the shared database gate before reading permissions or counting admins.
	// READ COMMITTED gives the following queries a fresh snapshot after any wait.
	if _, err := queries.LockUserManagement(ctx); err != nil {
		_ = tx.Rollback(context.Background())
		return nil, nil, fmt.Errorf("lock user management: %w", err)
	}
	if err := authorizeUserManagement(ctx, queries, actorID, permission); err != nil {
		_ = tx.Rollback(context.Background())
		return nil, nil, err
	}
	return tx, queries, nil
}

func authorizeUserManagement(ctx context.Context, queries *dbgen.Queries, actorID int64, permission authz.Permission) error {
	actor, err := managedUser(ctx, queries, actorID)
	if errors.Is(err, ErrUserNotFound) {
		return ErrUnauthenticated
	}
	if err != nil {
		return err
	}
	if actor.Status != "active" {
		return ErrUnauthenticated
	}
	return authz.Authorize(actor.Role, permission)
}

func preserveActiveAdmin(ctx context.Context, queries *dbgen.Queries, user User, status, role string) error {
	if user.Status != "active" || user.Role != "admin" || (status == "active" && role == "admin") {
		return nil
	}
	count, err := queries.CountActiveAdmins(ctx)
	if err != nil {
		return fmt.Errorf("count active administrators: %w", err)
	}
	if count <= 1 {
		return ErrLastAdmin
	}
	return nil
}

func managedUser(ctx context.Context, queries *dbgen.Queries, id int64) (User, error) {
	row, err := queries.GetManagedUser(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get managed user: %w", err)
	}
	return publicManagedUser(row), nil
}

func publicManagedUser(row dbgen.GetManagedUserRow) User {
	return User{
		ID:            row.ID,
		Username:      row.Username,
		Email:         row.Email.String,
		EmailVerified: row.EmailVerifiedAt.Valid,
		Status:        row.Status,
		Role:          row.Role,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func userCreationError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "users_username_key":
			return ErrUsernameTaken
		case "users_email_key":
			return ErrEmailTaken
		}
	}
	return err
}

func invalidUserField(field, detail string) error {
	return &ValidationError{
		Fields: []FieldError{{
			Field:  field,
			Code:   "invalid_value",
			Detail: detail,
		}},
	}
}
