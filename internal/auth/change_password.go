package auth

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/PazVz/mini_coder/internal/database/dbgen"
)

var (
	// ErrInvalidCurrentPassword indicates a failed password confirmation.
	ErrInvalidCurrentPassword = errors.New("current password is incorrect")
	// ErrPasswordNotSet prevents password changes from creating a new local authenticator.
	ErrPasswordNotSet = errors.New("account has no local password")
)

// PasswordChange confirms the existing local credential and supplies its replacement.
type PasswordChange struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func validatePasswordChange(change PasswordChange) error {
	var fields []FieldError
	// Confirmation accepts older password policies, but keeps work and input bounded.
	if !utf8.ValidString(change.CurrentPassword) || len(change.CurrentPassword) == 0 || len(change.CurrentPassword) > 1024 {
		fields = append(fields, FieldError{
			Field:  "current_password",
			Code:   "invalid_length",
			Detail: "当前密码须为 1～1024 字节的有效 UTF-8 文本",
		})
	}
	if !validPassword(change.NewPassword) {
		fields = append(fields, FieldError{
			Field:  "new_password",
			Code:   "invalid_length",
			Detail: "新密码至少 12 个字符，最多 1024 字节",
		})
	}
	if len(fields) != 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// ChangePassword updates the authenticated user's local password and revokes all sessions.
// userID comes from the authenticated HTTP context; the session is rechecked under the
// same user row lock used by Login, including after waiting for another password change.
func (s *Service) ChangePassword(ctx context.Context, userID int64, token string, change PasswordChange) error {
	if !validToken(token) {
		return ErrUnauthenticated
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin password change: %w", err)
	}
	defer tx.Rollback(context.Background())
	queries := s.queries.WithTx(tx)
	user, err := queries.LockAuthUserByID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthenticated
	}
	if err != nil {
		return fmt.Errorf("lock password user: %w", err)
	}
	sessionUser, err := queries.GetSessionUser(ctx, tokenHash(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthenticated
	}
	if err != nil {
		return fmt.Errorf("recheck password session: %w", err)
	}
	if sessionUser.ID != user.ID {
		return ErrUnauthenticated
	}
	if err := validatePasswordChange(change); err != nil {
		return err
	}
	if !user.PasswordHash.Valid {
		return ErrPasswordNotSet
	}
	match, err := verifyPassword(user.PasswordHash.String, change.CurrentPassword)
	if err != nil {
		return fmt.Errorf("verify current password: %w", err)
	}
	if !match {
		return ErrInvalidCurrentPassword
	}
	// Migration 00004's trigger deletes sessions in this transaction, including the caller's.
	if err := queries.UpdatePassword(ctx, dbgen.UpdatePasswordParams{
		ID:           user.ID,
		PasswordHash: pgtype.Text{String: hashPassword(change.NewPassword), Valid: true},
	}); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit password change: %w", err)
	}
	return nil
}
